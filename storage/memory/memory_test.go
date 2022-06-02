package memory

import (
	"github.com/tie-core/storage"
	"testing"
)

func TestStorage(t *testing.T) {
	t.Helper()

	f := func(t *testing.T) (storage.Storage, func()) {
		t.Helper()

		s, _ := NewMemoryStorage()

		return s, func() {}
	}
	storage.TestStorage(t, f)
}