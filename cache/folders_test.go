package cache

import (
	"testing"
	"time"

	"github.com/bubblmail/bubblmail/data"
)

func TestMoveMessageUpdatesFolderUIDAndPreservesBody(t *testing.T) {
	store := openTestStore(t)
	msg := &data.Message{
		AccountName: "work",
		FolderName:  "INBOX",
		UID:         7,
		Subject:     "Move me",
		Date:        time.Now(),
	}
	if err := store.UpsertMessages([]*data.Message{msg}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}
	if err := store.UpsertBody(msg.ID, "cached body", ""); err != nil {
		t.Fatalf("UpsertBody: %v", err)
	}

	if err := store.MoveMessage("work", "INBOX", 7, "Archive/Done", 42); err != nil {
		t.Fatalf("MoveMessage: %v", err)
	}

	moved, err := store.GetMessageByUID("work", "Archive/Done", 42)
	if err != nil {
		t.Fatalf("GetMessageByUID moved: %v", err)
	}
	body, _, err := store.GetBody(moved.ID)
	if err != nil {
		t.Fatalf("GetBody moved: %v", err)
	}
	if body != "cached body" {
		t.Fatalf("body = %q, want cached body", body)
	}
	if _, err := store.GetMessageByUID("work", "INBOX", 7); err == nil {
		t.Fatal("message still exists in source folder")
	}
}

func TestMoveMessageDeletesSourceWhenDestinationUIDUnknown(t *testing.T) {
	store := openTestStore(t)
	msg := &data.Message{
		AccountName: "work",
		FolderName:  "INBOX",
		UID:         7,
		Subject:     "Move me",
		Date:        time.Now(),
	}
	if err := store.UpsertMessages([]*data.Message{msg}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	if err := store.MoveMessage("work", "INBOX", 7, "Archive/Done", 0); err != nil {
		t.Fatalf("MoveMessage: %v", err)
	}

	if _, err := store.GetMessageByUID("work", "INBOX", 7); err == nil {
		t.Fatal("message still exists in source folder")
	}
	if _, err := store.GetMessageByUID("work", "Archive/Done", 7); err == nil {
		t.Fatal("message was cached in destination with stale source uid")
	}
}

func TestRenameFolderUpdatesFoldersAndMessages(t *testing.T) {
	store := openTestStore(t)
	if err := store.UpsertFolders("work", []*data.Folder{{Name: "Old/Child", DisplayName: "Child", Delimiter: "/", AccountName: "work"}}); err != nil {
		t.Fatalf("UpsertFolders: %v", err)
	}
	msg := &data.Message{
		AccountName: "work",
		FolderName:  "Old/Child",
		UID:         8,
		Subject:     "Rename me",
		Date:        time.Now(),
	}
	if err := store.UpsertMessages([]*data.Message{msg}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}

	if err := store.RenameFolder("work", "Old/Child", "New/Child"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}

	if _, err := store.GetMessageByUID("work", "New/Child", 8); err != nil {
		t.Fatalf("GetMessageByUID renamed folder: %v", err)
	}
	folders, err := store.GetFolders("work")
	if err != nil {
		t.Fatalf("GetFolders: %v", err)
	}
	if len(folders) != 1 || folders[0].Name != "New/Child" || folders[0].DisplayName != "Child" || folders[0].Depth != 1 {
		t.Fatalf("folders = %#v, want renamed nested folder", folders)
	}
}

func TestRenameFolderUpdatesDescendantFoldersMessagesAndEmbeddings(t *testing.T) {
	store := openTestStore(t)
	if err := store.UpsertFolders("work", []*data.Folder{
		{Name: "Old", DisplayName: "Old", Delimiter: "/", AccountName: "work"},
		{Name: "Old/Child", DisplayName: "Child", Delimiter: "/", AccountName: "work"},
		{Name: "Old/Child/Grandchild", DisplayName: "Grandchild", Delimiter: "/", AccountName: "work"},
	}); err != nil {
		t.Fatalf("UpsertFolders: %v", err)
	}
	msg := &data.Message{
		AccountName: "work",
		FolderName:  "Old/Child",
		UID:         8,
		Subject:     "Rename me",
		Date:        time.Now(),
	}
	if err := store.UpsertMessages([]*data.Message{msg}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}
	if err := store.UpsertFolderEmbedding("work", "Old/Child", "test", []float32{1, 2}, 2, 1); err != nil {
		t.Fatalf("UpsertFolderEmbedding: %v", err)
	}

	if err := store.RenameFolder("work", "Old", "New/Root"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}

	if _, err := store.GetMessageByUID("work", "New/Root/Child", 8); err != nil {
		t.Fatalf("GetMessageByUID renamed descendant folder: %v", err)
	}
	folders, err := store.GetFolders("work")
	if err != nil {
		t.Fatalf("GetFolders: %v", err)
	}
	got := map[string]int{}
	for _, folder := range folders {
		got[folder.Name] = folder.Depth
	}
	want := map[string]int{
		"New/Root":                  1,
		"New/Root/Child":            2,
		"New/Root/Child/Grandchild": 3,
	}
	if len(got) != len(want) {
		t.Fatalf("folders = %#v, want %v", folders, want)
	}
	for name, depth := range want {
		if got[name] != depth {
			t.Fatalf("folder %s depth = %d, want %d; all folders %#v", name, got[name], depth, folders)
		}
	}
	embeddingFolders, _, _, _, err := store.ListFolderEmbeddings("work", "test")
	if err != nil {
		t.Fatalf("ListFolderEmbeddings: %v", err)
	}
	if len(embeddingFolders) != 1 || embeddingFolders[0].Name != "New/Root/Child" {
		t.Fatalf("embedding folders = %#v, want renamed descendant", embeddingFolders)
	}
}

func TestDeleteFolderRemovesFolderMessagesAndBodies(t *testing.T) {
	store := openTestStore(t)
	if err := store.UpsertFolders("work", []*data.Folder{{Name: "Trash/Old", DisplayName: "Old", Delimiter: "/", AccountName: "work"}}); err != nil {
		t.Fatalf("UpsertFolders: %v", err)
	}
	msg := &data.Message{
		AccountName: "work",
		FolderName:  "Trash/Old",
		UID:         9,
		Subject:     "Delete me",
		Date:        time.Now(),
	}
	if err := store.UpsertMessages([]*data.Message{msg}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}
	if err := store.UpsertBody(msg.ID, "cached body", ""); err != nil {
		t.Fatalf("UpsertBody: %v", err)
	}

	if err := store.DeleteFolder("work", "Trash/Old"); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}

	if _, err := store.GetMessageByUID("work", "Trash/Old", 9); err == nil {
		t.Fatal("message still exists after folder delete")
	}
	body, _, err := store.GetBody(msg.ID)
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	if body != "" {
		t.Fatalf("body = %q, want empty after cascade delete", body)
	}
	folders, err := store.GetFolders("work")
	if err != nil {
		t.Fatalf("GetFolders: %v", err)
	}
	if len(folders) != 0 {
		t.Fatalf("folders = %#v, want none", folders)
	}
}
