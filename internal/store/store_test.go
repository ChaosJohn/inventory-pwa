package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := New(db)
	if err := st.CreateUser("Tester", "tester", "13800000000", "secret", "member", "active"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestConsumePrefersOldestBatch(t *testing.T) {
	st := newTestStore(t)
	user, _ := st.UserByUsername("tester")
	loc, err := st.SaveLocation(Location{Name: "储物柜"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := st.SaveItem(Item{Name: "纸巾", Unit: "包", MinStock: 2})
	if err != nil {
		t.Fatal(err)
	}
	oldBatch, err := st.AddBatch(user.ID, Batch{ItemID: item.ID, LocationID: loc.ID, InitialQuantity: 5, PurchasedAt: time.Now().Add(-48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	newBatch, err := st.AddBatch(user.ID, Batch{ItemID: item.ID, LocationID: loc.ID, InitialQuantity: 3, PurchasedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Consume(user.ID, item.ID, 0, 4, "日常使用"); err != nil {
		t.Fatal(err)
	}
	gotOld, _ := st.Batch(oldBatch.ID)
	gotNew, _ := st.Batch(newBatch.ID)
	if gotOld.CurrentQuantity != 1 {
		t.Fatalf("oldest batch should be consumed first, got %v", gotOld.CurrentQuantity)
	}
	if gotNew.CurrentQuantity != 3 {
		t.Fatalf("newer batch should remain untouched, got %v", gotNew.CurrentQuantity)
	}
}

func TestLowStockIsComputedFromActiveBatches(t *testing.T) {
	st := newTestStore(t)
	user, _ := st.UserByUsername("tester")
	loc, _ := st.SaveLocation(Location{Name: "厨房"})
	item, err := st.SaveItem(Item{Name: "洗碗块", Unit: "盒", MinStock: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBatch(user.ID, Batch{ItemID: item.ID, LocationID: loc.ID, InitialQuantity: 3}); err != nil {
		t.Fatal(err)
	}
	low, err := st.ListItems("", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(low) != 0 {
		t.Fatalf("item with stock above threshold should not be low stock")
	}
	if err := st.Consume(user.ID, item.ID, 0, 1, "用掉一盒"); err != nil {
		t.Fatal(err)
	}
	low, err = st.ListItems("", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(low) != 1 || low[0].ID != item.ID {
		t.Fatalf("expected item to become low stock, got %+v", low)
	}
}

func TestItemTotalCostComesFromBatches(t *testing.T) {
	st := newTestStore(t)
	user, _ := st.UserByUsername("tester")
	loc, _ := st.SaveLocation(Location{Name: "储物柜"})
	item, err := st.SaveItem(Item{Name: "猫粮", Owner: "宠物专用", Unit: "袋"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBatch(user.ID, Batch{ItemID: item.ID, LocationID: loc.ID, InitialQuantity: 1, Cost: 99.5}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBatch(user.ID, Batch{ItemID: item.ID, LocationID: loc.ID, InitialQuantity: 2, Cost: 120}); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := st.GetItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Owner != "宠物专用" {
		t.Fatalf("expected owner to be saved, got %q", got.Owner)
	}
	if got.TotalCost != 219.5 {
		t.Fatalf("expected total cost 219.5, got %v", got.TotalCost)
	}
}

func TestSetBatchPhoto(t *testing.T) {
	st := newTestStore(t)
	user, _ := st.UserByUsername("tester")
	loc, _ := st.SaveLocation(Location{Name: "储物柜"})
	item, err := st.SaveItem(Item{Name: "湿巾", Unit: "包"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := st.AddBatch(user.ID, Batch{ItemID: item.ID, LocationID: loc.ID, InitialQuantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetBatchPhoto(batch.ID, "/uploads/batches/test.jpg"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Batch(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PhotoPath != "/uploads/batches/test.jpg" {
		t.Fatalf("expected batch photo path to be saved, got %q", got.PhotoPath)
	}
}

func TestConsumeCanTargetLocation(t *testing.T) {
	st := newTestStore(t)
	user, _ := st.UserByUsername("tester")
	kitchen, err := st.SaveLocation(Location{Name: "厨房柜"})
	if err != nil {
		t.Fatal(err)
	}
	bathroom, err := st.SaveLocation(Location{Name: "卫生间柜"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := st.SaveItem(Item{Name: "抽纸", Unit: "包"})
	if err != nil {
		t.Fatal(err)
	}
	kitchenBatch, err := st.AddBatch(user.ID, Batch{ItemID: item.ID, LocationID: kitchen.ID, InitialQuantity: 5, PurchasedAt: time.Now().Add(-48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	bathroomBatch, err := st.AddBatch(user.ID, Batch{ItemID: item.ID, LocationID: bathroom.ID, InitialQuantity: 3, PurchasedAt: time.Now().Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Consume(user.ID, item.ID, bathroom.ID, 2, "从卫生间取用"); err != nil {
		t.Fatal(err)
	}
	gotKitchen, _ := st.Batch(kitchenBatch.ID)
	gotBathroom, _ := st.Batch(bathroomBatch.ID)
	if gotKitchen.CurrentQuantity != 5 {
		t.Fatalf("kitchen stock should remain untouched, got %v", gotKitchen.CurrentQuantity)
	}
	if gotBathroom.CurrentQuantity != 1 {
		t.Fatalf("bathroom stock should be consumed, got %v", gotBathroom.CurrentQuantity)
	}
	movements, err := st.MovementsForItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if movements[0].FromLocationID == nil || *movements[0].FromLocationID != bathroom.ID {
		t.Fatalf("consume movement should record source location, got %+v", movements[0].FromLocationID)
	}
}

func TestSaveItemRejectsDuplicateIdentity(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.SaveItem(Item{Name: "洗衣液", Brand: "蓝月亮", Spec: "2kg/瓶", Unit: "瓶"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveItem(Item{Name: "洗衣液", Brand: "蓝月亮", Spec: "2kg/瓶", Unit: "瓶"}); !errors.Is(err, ErrDuplicateItemName) {
		t.Fatalf("expected duplicate item error, got %v", err)
	}
	if _, err := st.SaveItem(Item{Name: "洗衣液", Brand: "蓝月亮", Spec: "1kg/瓶", Unit: "瓶"}); err != nil {
		t.Fatalf("same name and brand with different spec should be allowed: %v", err)
	}
}

func TestDeleteItemRemovesItem(t *testing.T) {
	st := newTestStore(t)
	item, err := st.SaveItem(Item{Name: "牙膏", Unit: "支"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteItem(item.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.GetItem(item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected item to be deleted, got %v", err)
	}
}

func TestSessionPersistsUntilExpiry(t *testing.T) {
	st := newTestStore(t)
	user, _ := st.UserByUsername("tester")
	if err := st.CreateSession("token-1", user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := st.SessionUser("token-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != user.ID {
		t.Fatalf("expected session user %d, got %d", user.ID, got.ID)
	}
	if err := st.CreateSession("token-2", user.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionUser("token-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expired session to be rejected, got %v", err)
	}
}

func TestUserByLoginSupportsAdminAndPhone(t *testing.T) {
	st := newTestStore(t)
	adminHash := "admin-secret"
	if err := st.CreateUser("Admin", "admin", "admin", adminHash, "admin", "active"); err != nil {
		t.Fatal(err)
	}
	admin, err := st.UserByLogin("admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.Role != "admin" {
		t.Fatalf("expected admin login to return admin, got %q", admin.Role)
	}
	member, err := st.UserByLogin("13800000000")
	if err != nil {
		t.Fatal(err)
	}
	if member.Username != "tester" {
		t.Fatalf("expected phone login to return tester, got %q", member.Username)
	}
}

func TestSaveMemberRejectsDuplicatePhoneOrUsername(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.SaveMember(User{Username: "wife", Phone: "13900000000", Status: "active"}, "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveMember(User{Username: "wife", Phone: "13700000000", Status: "active"}, "secret"); !errors.Is(err, ErrDuplicateUser) {
		t.Fatalf("expected duplicate username error, got %v", err)
	}
	if _, err := st.SaveMember(User{Username: "husband", Phone: "13900000000", Status: "active"}, "secret"); !errors.Is(err, ErrDuplicateUser) {
		t.Fatalf("expected duplicate phone error, got %v", err)
	}
}
