package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sreenivas-sadhu-prabhakara/rule-37-180-day-itc/internal/rule37"
)

// fileVersion is stamped into the data file so a later version of the tool can
// recognise what it is reading.
const fileVersion = 1

// ledger is everything the tool keeps: whose register it is, the figures in
// force, and the invoices with their payments. It is the entire contents of the
// data file, and the data file is the whole database.
type ledger struct {
	Version  int              `json:"version"`
	Business rule37.Business  `json:"business"`
	Settings rule37.Settings  `json:"settings"`
	Invoices []rule37.Invoice `json:"invoices"`
	Updated  string           `json:"updated,omitempty"`
}

// store holds the ledger in memory and writes it to one local JSON file.
// Nothing is sent anywhere.
type store struct {
	mu   sync.Mutex
	path string
	data ledger
	now  func() time.Time
}

// openStore reads the data file, or starts an empty ledger when there is none
// yet. A file that exists but cannot be parsed is an error rather than
// something to overwrite: the user's records are not ours to discard.
func openStore(path string) (*store, error) {
	s := &store{
		path: path,
		data: ledger{Version: fileVersion, Settings: rule37.DefaultSettings()},
		now:  time.Now,
	}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var data ledger
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("%s is not a data file this tool wrote: %w", path, err)
	}
	// An older file may be missing settings this version knows about, so the
	// statutory defaults fill the gaps rather than nil figures reaching the
	// arithmetic.
	data.Settings = data.Settings.Normalise()
	if data.Settings.InterestRatePct == 0 && data.Version < fileVersion {
		data.Settings.InterestRatePct = rule37.InterestRateAnnualPctDefault
	}
	data.Version = fileVersion
	s.data = data
	return s, nil
}

// snapshot returns a copy of the ledger the caller can read freely.
func (s *store) snapshot() ledger {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.copyLocked()
}

// update applies fn to the ledger and writes the file. The ledger is only
// changed in memory if the write succeeds, so a failed write leaves the tool
// and the file saying the same thing.
func (s *store) update(fn func(*ledger) error) (ledger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.copyLocked()
	if err := fn(&next); err != nil {
		return ledger{}, err
	}
	next.Updated = s.now().UTC().Format(time.RFC3339)
	previous := s.data
	s.data = next
	if err := s.writeLocked(); err != nil {
		s.data = previous
		return ledger{}, err
	}
	return s.copyLocked(), nil
}

func (s *store) copyLocked() ledger {
	out := s.data
	out.Invoices = make([]rule37.Invoice, len(s.data.Invoices))
	for i, inv := range s.data.Invoices {
		out.Invoices[i] = inv
		out.Invoices[i].Payments = append([]rule37.Payment(nil), inv.Payments...)
	}
	out.Settings.BucketEdges = append([]int(nil), s.data.Settings.BucketEdges...)
	return out
}

// writeLocked writes the ledger to a temporary file in the same directory and
// renames it over the data file, so an interrupted write cannot truncate the
// user's records. The temporary file is created readable only by its owner,
// which the rename carries over.
func (s *store) writeLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".rule-37-itc-*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename has succeeded

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("replacing %s: %w", s.path, err)
	}
	return nil
}

// newID returns a short random key for an invoice record. It identifies the row
// in the local file and in the interface; it is not part of any statutory
// record.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; fall back to something still
		// unique rather than handing out an empty key.
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
