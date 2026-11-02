package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func capacityCard(index int) Item {
	return Item{ID: fmt.Sprintf("capacity-item-%06d", index), Name: fmt.Sprintf("Позиция %d", index), SKU: fmt.Sprintf("CAP-%d", index), Unit: "шт", Fields: map[string]string{}}
}

func TestStocktakingExpectedUnitAndLegacyCompatibility(t *testing.T) {
	a, _, token := fixture(t)
	seed(t, a, token, 0)
	item := a.st.S.Items["item-00001"]
	item.Unit = "шт"
	command(t, a, token, Command{ID: "stocktake-unit-change", Type: "item", Item: &item}, 200)
	seq := a.st.S.Seq
	row := Command{ID: "stocktake-unit-row1", Type: "count", ItemID: item.ID, To: "main-place", Qty: 3000, Expected: 0, ExpectedUnit: "кг", Note: "Пересчёт"}
	command(t, a, token, row, 409)
	valid := row
	valid.ID, valid.ExpectedUnit = "stocktake-unit-row2", "шт"
	command(t, a, token, Command{ID: "stocktake-unit-doc", Type: "batch", Commands: []Command{valid, row}}, 409)
	if a.st.S.Seq != seq || len(a.st.S.Events) != 0 || len(a.st.S.Stocks) != 0 {
		t.Fatal("unit conflict partially changed stock")
	}
	row.ExpectedUnit = ""
	command(t, a, token, row, 200)
	if a.st.S.Stocks[key(item.ID, "main-place")].Qty != 3000 {
		t.Fatal("legacy count command rejected")
	}
}

func TestCatalogEditorWithoutPriceAccessPreservesAuthoritativePrice(t *testing.T) {
	a, owner, token := fixture(t)
	seed(t, a, token, 0)
	item := a.st.S.Items["item-00001"]
	item.Price = 12345
	command(t, a, token, Command{ID: "owner-price-change", Type: "item", Item: &item}, 200)
	editor := owner
	editor.ID, editor.Role, editor.Permissions = "price-blind-editor", "manager", map[string]bool{permViewPrices: false}
	item = a.st.S.Items[item.ID]
	item.Name, item.Price = "Новое название", 0
	patch, err := prepare(&a.st.S, editor, Command{ID: "hidden-price-edit", Type: "item", Item: &item})
	if err != nil {
		t.Fatal(err)
	}
	if patch.Item.Price != 12345 || patch.Item.Name != "Новое название" {
		t.Fatal("hidden price was lost")
	}
	apply(&a.st.S, patch)
	newItem := capacityCard(100001)
	newItem.Price = 99999
	patch, err = prepare(&a.st.S, editor, Command{ID: "hidden-price-new1", Type: "item", Item: &newItem})
	if err != nil || patch.Item.Price != 0 {
		t.Fatal("price-blind editor set a new price")
	}
	patch, err = prepare(&a.st.S, editor, Command{ID: "hidden-price-bulk", Type: "catalog-import", Items: []Item{newItem}})
	if err != nil || len(patch.Batch) != 1 || patch.Batch[0].Item.Price != 0 {
		t.Fatal("price-blind editor imported prices")
	}
}

func TestBatchAtomicFailureRetryAndRestart(t *testing.T) {
	a, owner, token := fixture(t)
	seed(t, a, token, 5000)
	seq := a.st.S.Seq
	document := Command{ID: "document-atomic-001", Type: "batch", Ref: "Накладная 7", Note: "Поставка", Commands: []Command{
		{ID: "document-row-001", Type: "in", ItemID: "item-00001", To: "main-place", Qty: 3000},
		{ID: "document-row-002", Type: "out", ItemID: "item-00001", From: "main-place", Qty: 9000},
	}}
	command(t, a, token, document, 409)
	if a.st.S.Seq != seq || a.st.S.Stocks[key("item-00001", "main-place")].Qty != 5000 {
		t.Fatal("failed document partially changed state")
	}
	document.Commands[1].Qty = 2000
	command(t, a, token, document, 200)
	if a.st.S.Seq != seq+1 || a.st.S.Stocks[key("item-00001", "main-place")].Qty != 6000 {
		t.Fatal("document did not commit as one revision")
	}
	events := recentHistory(&a.st.S, 2)
	if events[0].Seq != events[1].Seq || events[0].Ref != document.Ref {
		t.Fatal("document audit fields missing")
	}
	command(t, a, token, document, 200)
	if a.st.S.Seq != seq+1 {
		t.Fatal("retry duplicated the document")
	}
	changed := document
	changed.Commands = append([]Command(nil), document.Commands...)
	changed.Commands[0].Qty++
	command(t, a, token, changed, 409)
	newIDSameRows := document
	newIDSameRows.ID = "document-copy-002"
	command(t, a, token, newIDSameRows, 400)
	viewer := owner
	viewer.Role = "viewer"
	if _, err := prepare(&a.st.S, viewer, Command{ID: "viewer-batch-001", Type: "batch", Commands: []Command{{ID: "viewer-line-001", Type: "in", ItemID: "item-00001", To: "main-place", Qty: 1000}}}); err == nil {
		t.Fatal("viewer could write a document")
	}
	before, _ := json.Marshal(a.st.S)
	if _, err := prepare(&a.st.S, owner, Command{ID: "stale-stocktaking", Type: "batch", Note: "Пересчёт", Commands: []Command{
		{ID: "stocktaking-row1", Type: "count", ItemID: "item-00001", To: "main-place", Qty: 4000, Expected: 3},
		{ID: "stocktaking-row2", Type: "count", ItemID: "item-00001", To: "main-place", Qty: 3000, Expected: 3},
	}}); err == nil {
		t.Fatal("stale count accepted")
	}
	after, _ := json.Marshal(a.st.S)
	if string(before) != string(after) {
		t.Fatal("stale stocktaking mutated state")
	}
	path := a.st.Path
	a.st.file.Close()
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.file.Close()
	if reopened.S.Seq != seq+1 || reopened.S.Stocks[key("item-00001", "main-place")].Qty != 6000 || historyCount(&reopened.S) != 3 {
		t.Fatal("document did not survive restart")
	}
}

func TestBulkCatalogImportAndDeclaredCapacity(t *testing.T) {
	a, owner, token := fixture(t)
	items := make([]Item, maxImportRows)
	for index := range items {
		items[index] = capacityCard(index)
	}
	document := Command{ID: "catalog-import-001", Type: "catalog-import", Items: items}
	seq := a.st.S.Seq
	command(t, a, token, document, 200)
	command(t, a, token, document, 200)
	if len(a.st.S.Items) != maxImportRows || a.st.S.Seq != seq+1 {
		t.Fatal("bulk import/retry lost or duplicated cards")
	}
	duplicate := Command{ID: "catalog-import-002", Type: "catalog-import", Items: []Item{capacityCard(10001), capacityCard(10002)}}
	duplicate.Items[1].SKU = duplicate.Items[0].SKU
	command(t, a, token, duplicate, 409)
	if len(a.st.S.Items) != maxImportRows {
		t.Fatal("bad catalog import partially committed")
	}
	for index := len(a.st.S.Items); index < maxItems; index++ {
		item := capacityCard(index)
		item.Version = 1
		a.st.S.Items[item.ID] = item
	}
	for index := len(a.st.S.Places); index < maxPlaces; index++ {
		id := fmt.Sprintf("capacity-place-%04d", index)
		a.st.S.Places[id] = Place{ID: id, Name: fmt.Sprintf("Ячейка %d", index), Version: 1}
	}
	item := capacityCard(maxItems + 1)
	if _, err := prepare(&a.st.S, owner, Command{ID: "capacity-overflow", Type: "item", Item: &item}); err == nil {
		t.Fatal("card limit not enforced")
	}
	if _, err := prepare(&a.st.S, owner, Command{ID: "place-overflow01", Type: "place", Place: &Place{ID: "place-overflow01", Name: "Лишнее место"}}); err == nil {
		t.Fatal("place limit not enforced")
	}
	snapshot := snapshot(&a.st.S, owner, false)
	limits := snapshot["limits"].(map[string]int)
	if limits["items"] != maxItems || limits["places"] != maxPlaces || !snapshot["capabilities"].(map[string]bool)["catalogImport"] {
		t.Fatal("snapshot limits/capabilities missing")
	}
	owner.Salt = "capacity-test-salt"
	owner.Hash = hash("capacity-test-password")
	a.st.S.Users[owner.ID] = owner
	if err := validateImportedState(a.st.S); err != nil {
		t.Fatalf("50k/2k host package rejected: %v", err)
	}
	duplicateCard := a.st.S.Items[capacityCard(49999).ID]
	duplicateCard.SKU = "cap-0"
	a.st.S.Items[duplicateCard.ID] = duplicateCard
	if err := validateImportedState(a.st.S); err == nil {
		t.Fatal("host package with duplicate SKU accepted")
	}
}

func TestBulkCatalogImportSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yarus.journal")
	store, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	host := activeHost("Компьютер", 1)
	if err = store.commit(Patch{Host: &host}); err != nil {
		t.Fatal(err)
	}
	user := User{ID: "owner-bulk-test", Role: "owner", Name: "Владелец"}
	items := make([]Item, 10000)
	for index := range items {
		items[index] = capacityCard(index)
	}
	patch, err := prepare(&store.S, user, Command{ID: "catalog-full-import", Type: "catalog-import", Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.commit(patch); err != nil {
		t.Fatal(err)
	}
	store.file.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte{'\n'})
	var first, document Record
	if json.Unmarshal(lines[0], &first) != nil || json.Unmarshal(lines[1], &document) != nil {
		t.Fatal("invalid journal records")
	}
	if first.Version != 1 || document.Version != 2 {
		t.Fatal("new document must have a distinguishable journal format")
	}
	if document.Hash == hash(first.Hash+string(document.Data)) || document.Hash == encryptedRecordHash(first.Hash, document.Nonce, document.Ciphertext) {
		t.Fatal("old readers could replay a new document as an empty patch")
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.file.Close()
	if len(reopened.S.Items) != 10000 || reopened.S.Items[capacityCard(9999).ID].Version != 1 {
		t.Fatal("bulk catalog lost on restart")
	}
}
