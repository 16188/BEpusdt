package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSetKRefreshesCacheAfterCommit(t *testing.T) {
	var openErr error
	Db, openErr = gorm.Open(sqlite.Open("file:setk-cache?mode=memory&cache=shared"), &gorm.Config{})
	if openErr != nil {
		t.Fatal(openErr)
	}
	if err := Db.AutoMigrate(&Conf{}); err != nil {
		t.Fatal(err)
	}

	key := ConfKey("test_setk_cache")
	if err := Db.Create(&Conf{K: key, V: "old"}).Error; err != nil {
		t.Fatal(err)
	}
	confCache.Store(key, "old")

	if err := SetK(key, "new"); err != nil {
		t.Fatal(err)
	}
	if cached := GetC(key); cached != "new" {
		t.Fatalf("expected immediate cache refresh, got %q", cached)
	}
	if stored := GetK(key); stored != "new" {
		t.Fatalf("expected committed value, got %q", stored)
	}
}
