package tools

import (
	"sync"
	"testing"
)

// Replace exists so an MCP config reload can change the tool set without
// restarting. It has to swap the whole set, not merge into it: a server that
// was removed from the config must take its tools with it.
func TestRegistryReplaceSwapsTheWholeSet(t *testing.T) {
	read, err := NewRead()
	if err != nil {
		t.Fatal(err)
	}
	glob, err := NewGlob()
	if err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(read)
	if _, ok := r.Lookup(read.Name()); !ok {
		t.Fatalf("%s missing before replace", read.Name())
	}

	r.Replace(glob)
	if _, ok := r.Lookup(glob.Name()); !ok {
		t.Errorf("%s missing after replace", glob.Name())
	}
	if _, ok := r.Lookup(read.Name()); ok {
		t.Errorf("%s survived a replace that did not include it", read.Name())
	}
	if got := r.Names(); len(got) != 1 {
		t.Errorf("Names() = %v, want exactly the replacement set", got)
	}
	if got := r.All(); len(got) != 1 {
		t.Errorf("All() returned %d tools, want 1", len(got))
	}
}

// The watcher replaces tools on its own goroutine while the agent loop reads
// them to build a request. Run under -race, this is the test that justifies the
// mutex; without it the map read and write are a data race.
func TestRegistryConcurrentReplaceAndRead(t *testing.T) {
	read, err := NewRead()
	if err != nil {
		t.Fatal(err)
	}
	glob, err := NewGlob()
	if err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(read)
	stop := make(chan struct{})

	var reloader sync.WaitGroup
	reloader.Add(1)
	go func() {
		defer reloader.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				r.Replace(read, glob)
			} else {
				r.Replace(read)
			}
		}
	}()

	var readers sync.WaitGroup
	for i := 0; i < 3; i++ {
		readers.Add(1)
		go func() { // readers, as the loop does each turn
			defer readers.Done()
			for j := 0; j < 2000; j++ {
				r.Lookup(read.Name())
				r.Names()
				r.All()
			}
		}()
	}

	readers.Wait()
	close(stop)
	reloader.Wait()
}
