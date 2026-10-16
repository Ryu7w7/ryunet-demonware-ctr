package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Currency IDs are assigned from CMarketplaceData's baseCurrencyId (10).
const wumpaCurrency = uint64(10)

//go:embed data/ctr-skus.json
var catalogJSON []byte

type economySKU struct {
	Items []uint32 `json:"items"`
	Parts []uint32 `json:"parts"`
	Price int64    `json:"full_price"`
}

var economyCatalog = func() map[uint32]economySKU {
	m := map[uint32]economySKU{}
	if err := json.Unmarshal(catalogJSON, &m); err != nil {
		panic(err)
	}
	return m
}()

// One atomic file contains both the debit and item grant. Legacy files remain
// untouched for rollback; after migration only this versioned file is read.
type economyAccount struct {
	Challenges     map[string]challengeCompletion `json:"challenges,omitempty"`
	PitStopRefresh *pitStopRefreshRecord          `json:"pitstop_refresh,omitempty"`
	Version        int                            `json:"version"`
	Balances       map[uint64]int64               `json:"balances"`
	Items          map[uint32]bool                `json:"items"`
	RaceReceipts   map[string]bool                `json:"race_receipts,omitempty"`
	OnlineSeconds  map[string]uint64              `json:"online_seconds,omitempty"`
}

type economyStore struct {
	mu  sync.Mutex
	dir string
}

var economy = &economyStore{}

func readEconomyJSON(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}

func (s *economyStore) accountPath(pid uint64) string {
	return filepath.Join(s.dir, strconv.FormatUint(pid, 10)+".economy-v2.json")
}

func (s *economyStore) load(pid uint64) (*economyAccount, error) {
	a := &economyAccount{}
	err := readEconomyJSON(s.accountPath(pid), a)
	if err == nil {
		if a.Version != 2 || a.Balances == nil || a.Items == nil {
			return nil, fmt.Errorf("invalid economy account")
		}
		return a, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if pid != 896289731 {
		if legacyErr := readEconomyJSON(s.accountPath(896289731), a); legacyErr == nil && a.Version == 2 && a.Balances != nil {
			_ = s.save(pid, a)
			return a, nil
		}
	}
	a = &economyAccount{Version: 2, Balances: map[uint64]int64{}, Items: map[uint32]bool{}}
	base := filepath.Join(s.dir, strconv.FormatUint(pid, 10))
	if err := readEconomyJSON(base+".json", &a.Balances); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if a.Balances == nil {
		return nil, fmt.Errorf("null legacy wallet")
	}
	if _, ok := a.Balances[wumpaCurrency]; !ok {
		a.Balances[wumpaCurrency] = startingBalance()
	}
	var skus []uint32
	if err := readEconomyJSON(base+".items.json", &skus); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, id := range skus {
		entry, ok := economyCatalog[id]
		if !ok {
			return nil, fmt.Errorf("unknown legacy SKU %d", id)
		}
		for _, item := range entry.Items {
			a.Items[item] = true
		}
	}
	if err := s.save(pid, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *economyStore) save(pid uint64, a *economyAccount) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".economy-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, s.accountPath(pid))
}

func (s *economyStore) snapshot(pid uint64) (*economyAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(pid)
}

func ownsSKU(a *economyAccount, sku economySKU) bool {
	if len(sku.Items) == 0 {
		return false
	}
	for _, id := range sku.Items {
		if !a.Items[id] {
			return false
		}
	}
	return true
}

func skuPrice(a *economyAccount, sku economySKU) (int64, error) {
	if ownsSKU(a, sku) {
		return 0, nil
	}
	if len(sku.Parts) == 0 {
		return sku.Price, nil
	}
	var price int64
	// CMarketplaceBundleSku::getDiscountPrice sums unowned component prices.
	for _, id := range sku.Parts {
		part, ok := economyCatalog[id]
		if !ok {
			return 0, fmt.Errorf("missing component %d", id)
		}
		if !ownsSKU(a, part) {
			price += part.Price
		}
	}
	return price, nil
}

func (s *economyStore) purchase(pid uint64, skus []uint32) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.load(pid)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, id := range skus {
		sku, ok := economyCatalog[id]
		if !ok {
			return 0, fmt.Errorf("unknown SKU %d", id)
		}
		price, err := skuPrice(a, sku)
		if err != nil {
			return 0, err
		}
		if price < 0 || price > a.Balances[wumpaCurrency] {
			return 0, fmt.Errorf("insufficient Wumpa Coins")
		}
		a.Balances[wumpaCurrency] -= price
		total += price
		for _, item := range sku.Items {
			a.Items[item] = true
		}
	}
	if err := s.save(pid, a); err != nil {
		return 0, err
	}
	return total, nil
}

// inventoryMode decides which Pit Stop items the game sees as owned while online
// (CTR_INVENTORY). The game keeps two inventories: its save's (_localMarketplaceInventory)
// and Demonware's (_dwInventoryItems), and online it goes by Demonware's once it gets one;
// after a purchase it asks for Demonware's again to show what was bought. Until octane_psn
// was accepted every inventory request failed, so PS4/PC players always played with their
// save's items, imported "100%" saves included.
//
//	server  items bought on this server, per nickname (default). A player can still
//	        choose "save" for themselves in the launcher (inventoryHeader).
//	all     every catalog item, for everyone
//	save    everyone keeps their save's items, as before ("local" is the same)
func inventoryMode() string {
	switch m := strings.ToLower(strings.TrimSpace(os.Getenv("CTR_INVENTORY"))); m {
	case "all", "save":
		return m
	case "local":
		return "save"
	}
	return "server"
}

// inventoryHeader carries the launcher's per-player choice at login: "save" keeps the Pit
// Stop items of the player's own save (a "100%" save, say) instead of the ones bought on
// the server. Those players buy nothing, since the game would only show a purchase through
// the server's inventory: their coins are never spent on an item they would not see.
const inventoryHeader = "X-RyuNET-Inventory"

// inventoryModeFor is the server-wide mode, or this player's choice when it is "server".
func (l *lobbyConn) inventoryModeFor() string {
	if m := inventoryMode(); m != "server" {
		return m
	}
	if l.player != nil && l.player.SaveInventory {
		return "save"
	}
	return "server"
}

// allCatalogItems is every item a Pit Stop SKU can grant, sorted (CTR_INVENTORY=all).
var allCatalogItems = sync.OnceValue(func() []uint32 {
	seen := map[uint32]bool{}
	var items []uint32
	for _, sku := range economyCatalog {
		for _, id := range sku.Items {
			if !seen[id] {
				seen[id] = true
				items = append(items, id)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
	return items
})

func inventoryPage(a *economyAccount, page, size uint32) []uint32 {
	items := make([]uint32, 0, len(a.Items))
	for id, owned := range a.Items {
		if owned {
			items = append(items, id)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
	return pageOf(items, page, size)
}

// pageOf returns one page of a sorted item list; pages count from 1.
func pageOf(items []uint32, page, size uint32) []uint32 {
	// CMarketplaceGetInventoryTask::start explicitly requests page 1 first.
	if page == 0 {
		return nil
	}
	start := uint64(page-1) * uint64(size)
	if start >= uint64(len(items)) {
		return nil
	}
	end := start + uint64(size)
	if end > uint64(len(items)) {
		end = uint64(len(items))
	}
	return items[start:end]
}
