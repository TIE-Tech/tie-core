package pvbft

import (
	"encoding/json"
	"fmt"
	"github.com/TIE-Tech/go-logger"
	"github.com/TIE-Tech/tie-core/common/crypto"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/TIE-Tech/tie-core/consensus/pvbft/proto"
	"github.com/TIE-Tech/tie-core/types"
)

// setupSnapshot sets up the snapshot store for the IBFT object
func (i *Ibft) setupSnapshot() error {
	i.store = newSnapshotStore()

	// Read from storage
	if i.config.Path != "" {
		if err := i.store.loadFromPath(i.config.Path); err != nil {
			return err
		}
	}

	header := i.blockchain.Header()
	meta, err := i.getSnapshotMetadata()
	if err != nil {
		return err
	}

	extra, err := getIbftExtra(header)
	if err != nil {
		return err
	}

	if header.Number == 0 {
		// Add genesis
		i.addHeaderSnap(header, extra.Validators)
	}

	// set validators and construct randpool
	i.state.vset.SetValidators(extra.Validators)

	// If the snapshot is not found, or the latest snapshot belongs to a previous epoch,
	// we need to start rebuilding the snapshot from the beginning of the current epoch
	// in order to have all the votes and validators correctly set in the snapshot,
	// since they reset every epoch.

	// Get epoch of latest header and saved metadata
	currentEpoch := header.Number / i.epochSize
	metaEpoch := meta.LastBlock / i.epochSize
	snapshot, _ := i.getSnapshot(header.Number)
	if snapshot == nil || metaEpoch < currentEpoch {
		// Restore snapshot at the beginning of the current epoch by block header
		// if list doesn't have any snapshots to calculate snapshot for the next header
		logger.Info("[BFT] snapshot was not found, restore snapshot at beginning of current epoch", "current epoch", currentEpoch)
		beginHeight := currentEpoch * i.epochSize
		beginHeader, ok := i.blockchain.GetHeaderByNumber(beginHeight)
		if !ok {
			return fmt.Errorf("header at %d not found", beginHeight)
		}

		i.addHeaderSnap(beginHeader, extra.Validators)
		i.store.updateLastBlock(beginHeight)

		if meta, err = i.getSnapshotMetadata(); err != nil {
			return err
		}
	}

	// Process headers if we missed some blocks in the current epoch
	if header.Number > meta.LastBlock {

		logger.Info("[BFT] syncing past snapshots", "from", meta.LastBlock, "to", header.Number)

		for num := meta.LastBlock + 1; num <= header.Number; num++ {
			if num == 0 {
				continue
			}

			header, ok := i.blockchain.GetHeaderByNumber(num)
			if !ok {
				return fmt.Errorf("header %d not found", num)
			}

			if err := i.processHeaders([]*types.Header{header}); err != nil {
				return err
			}
		}
	}

	return nil
}

// addHeaderSnap creates the initial snapshot, and adds it to the snapshot store
func (i *Ibft) addHeaderSnap(header *types.Header, validators []types.Address) {
	// Genesis header needs to be set by hand, all the other
	// snapshots are set as part of processHeaders
	// Create the first snapshot from the genesis
	snap := &Snapshot{
		Hash:   header.Hash.String(),
		Number: header.Number,
		Votes:  []*Vote{},
		Set:    validators,
	}
	i.store.add(snap)
}

// getLatestSnapshot returns the latest snapshot object
func (i *Ibft) getLatestSnapshot() (*Snapshot, error) {
	meta, err := i.getSnapshotMetadata()
	if err != nil {
		return nil, err
	}

	snap, err := i.getSnapshot(meta.LastBlock)
	if err != nil {
		return nil, err
	}

	return snap, nil
}

// processHeaders is the powerhouse method in the snapshot module.

// It processes passed in headers, and updates the snapshot / snapshot store
func (i *Ibft) processHeaders(headers []*types.Header) error {
	if len(headers) == 0 {
		return nil
	}

	parentSnap, err := i.getSnapshot(headers[0].Number - 1)
	if err != nil {
		return err
	}
	snap := parentSnap.Copy()

	// saveSnap is a callback function to set height and hash in current snapshot with given header
	// and store the snapshot to snapshot store
	saveSnap := func(h *types.Header) {
		snap.Number = h.Number
		snap.Hash = h.Hash.String()
		i.store.add(snap)

		// use saved snapshot as new parent and clone it for next
		parentSnap = snap
		snap = parentSnap.Copy()
	}

	for _, h := range headers {
		proposerPub, err := ecrecoverFromHeader(h)
		if err != nil {
			return err
		}
		proposer := crypto.PubKeyToAddress(proposerPub)

		// Check if the recovered proposer is part of the validator set
		if !snap.Includes(proposer) {
			return fmt.Errorf("unauthorized proposer")
		}
		saveSnap(h)
	}

	// update the metadata
	i.store.updateLastBlock(headers[len(headers)-1].Number)

	return nil
}

// getSnapshotMetadata returns the latest snapshot metadata
func (i *Ibft) getSnapshotMetadata() (*snapshotMetadata, error) {
	meta := &snapshotMetadata{
		LastBlock: i.store.getLastBlock(),
	}

	return meta, nil
}

// getSnapshot returns the snapshot at the specified block height
func (i *Ibft) getSnapshot(num uint64) (*Snapshot, error) {
	snap := i.store.find(num)

	return snap, nil
}

// Vote defines the vote structure
type Vote struct {
	Validator types.Address
	Address   types.Address
	Authorize bool
}

// Equal checks if two votes are equal
func (v *Vote) Equal(vv *Vote) bool {
	if v.Validator != vv.Validator {
		return false
	}

	if v.Address != vv.Address {
		return false
	}

	if v.Authorize != vv.Authorize {
		return false
	}

	return true
}

// Copy makes a copy of the vote, and returns it
func (v *Vote) Copy() *Vote {
	vv := new(Vote)
	*vv = *v

	return vv
}

// Snapshot is the current state at a given point in time for validators and votes
type Snapshot struct {
	// block number when the snapshot was created
	Number uint64

	// block hash when the snapshot was created
	Hash string

	// votes casted in chronological order
	Votes []*Vote

	// current set of validators
	Set []types.Address
}

// snapshotMetadata defines the metadata for the snapshot
type snapshotMetadata struct {
	// LastBlock represents the latest block in the snapshot
	LastBlock uint64
}

// Equal checks if two snapshots are equal
func (s *Snapshot) Equal(ss *Snapshot) bool {
	// we only check if Votes and Set are equal since Number and Hash
	// are only meant to be used for indexing
	if len(s.Votes) != len(ss.Votes) {
		return false
	}

	for indx := range s.Votes {
		if !s.Votes[indx].Equal(ss.Votes[indx]) {
			return false
		}
	}
	return s.SetEqual(ss.Set)
}

// SetEqual checks if 2 validator sets are equal
func (s *Snapshot) SetEqual(vv []types.Address) bool {
	if len(s.Set) != len(vv) {
		return false
	}

	for indx := range s.Set {
		if s.Set[indx] != vv[indx] {
			return false
		}
	}
	return true
}

// Index returns the index of the passed in address in the validator set.
// Returns -1 if not found
func (s *Snapshot) Index(addr types.Address) int {
	for indx, i := range s.Set {
		if i == addr {
			return indx
		}
	}
	return -1
}

// Includes checks if the address is in the validator set
func (s *Snapshot) Includes(addr types.Address) bool {
	return s.Index(addr) != -1
}

// Del removes an address from the validator set
func (s *Snapshot) Del(addr types.Address) {
	for indx, i := range s.Set {
		if i == addr {
			s.Set = append(s.Set[:indx], s.Set[indx+1:]...)
		}
	}
}

// MaxFaultyNodes returns the maximum number of allowed faulty nodes (F), based on the current validator set
func (s *Snapshot) MaxFaultyNodes() int {
	// N -> number of nodes in IBFT
	// F -> number of faulty nodes
	//
	// N = 3F + 1
	// => F = (N - 1) / 3
	//
	// IBFT tolerates 1 failure with 4 nodes
	// 4 = 3 * 1 + 1
	// To tolerate 2 failures, IBFT requires 7 nodes
	// 7 = 3 * 2 + 1
	// It should always take the floor of the result
	return (len(s.Set) - 1) / 3
}

// Count returns the vote tally.
// The count increases if the callback function returns true
func (s *Snapshot) Count(h func(v *Vote) bool) (count int) {
	for _, v := range s.Votes {
		if h(v) {
			count++
		}
	}
	return
}

// RemoveVotes removes votes from the snapshot, based on the passed in callback
func (s *Snapshot) RemoveVotes(h func(v *Vote) bool) {
	for i := 0; i < len(s.Votes); i++ {
		if h(s.Votes[i]) {
			s.Votes = append(s.Votes[:i], s.Votes[i+1:]...)
			i--
		}
	}
}

// Copy makes a copy of the snapshot
func (s *Snapshot) Copy() *Snapshot {
	// Do not need to copy Number and Hash
	ss := &Snapshot{
		Number: s.Number,
		Votes:  make([]*Vote, len(s.Votes)),
		Set:    make([]types.Address, 0),
	}

	for indx, vote := range s.Votes {
		ss.Votes[indx] = vote.Copy()
	}

	ss.Set = s.Set
	return ss
}

// ToProto converts the snapshot to a Proto snapshot
func (s *Snapshot) ToProto() *proto.Snapshot {
	resp := &proto.Snapshot{
		Validators: []*proto.Snapshot_Validator{},
		Votes:      []*proto.Snapshot_Vote{},
		Number:     s.Number,
		Hash:       s.Hash,
	}

	// add votes
	for _, vote := range s.Votes {
		resp.Votes = append(resp.Votes, &proto.Snapshot_Vote{
			Validator: vote.Validator.String(),
			Proposed:  vote.Address.String(),
			Auth:      vote.Authorize,
		})
	}

	// add addresses
	for _, val := range s.Set {
		resp.Validators = append(resp.Validators, &proto.Snapshot_Validator{
			Address: val.String(),
		})
	}
	return resp
}

// snapshotStore defines the structure of the stored snapshots
type snapshotStore struct {
	// lastNumber is the latest block number stored
	lastNumber uint64

	// lock is the snapshotStore mutex
	lock sync.Mutex

	// list represents the actual snapshot sorted list
	list snapshotSortedList
}

// newSnapshotStore returns a new snapshot store
func newSnapshotStore() *snapshotStore {
	return &snapshotStore{
		list: snapshotSortedList{},
	}
}

// loadFromPath loads a saved snapshot store from the specified file system path
func (s *snapshotStore) loadFromPath(path string) error {
	// Load metadata
	var meta *snapshotMetadata
	if err := readDataStore(filepath.Join(path, "metadata"), &meta); err != nil {
		// if we can't read metadata file delete it
		// and log the error that we've encountered
		logger.Error("Could not read metadata snapshot store file", "err", err.Error())
		os.Remove(filepath.Join(path, "metadata"))
		logger.Error("Removed invalid metadata snapshot store file")
	}

	if meta != nil {
		s.lastNumber = meta.LastBlock
	}

	// Load snapshots
	snaps := []*Snapshot{}
	if err := readDataStore(filepath.Join(path, "snapshots"), &snaps); err != nil {
		// if we can't read snapshot store file delete it
		// and log the error that we've encountered
		logger.Error("Could not read snapshot store file", "err", err.Error())
		os.Remove(filepath.Join(path, "snapshots"))
		logger.Error("Removed invalid snapshot store file")
	}

	for _, snap := range snaps {
		s.add(snap)
	}

	return nil
}

// saveToPath saves the snapshot store as a file to the specified path
func (s *snapshotStore) saveToPath(path string) error {
	// Write snapshots
	if err := writeDataStore(filepath.Join(path, "snapshots"), s.list); err != nil {
		return err
	}

	// Write metadata
	meta := &snapshotMetadata{
		LastBlock: s.lastNumber,
	}
	return writeDataStore(filepath.Join(path, "metadata"), meta)
}

// getLastBlock returns the latest block number from the snapshot store. [Thread safe]
func (s *snapshotStore) getLastBlock() uint64 {
	return atomic.LoadUint64(&s.lastNumber)
}

// updateLastBlock sets the latest block number in the snapshot store. [Thread safe]
func (s *snapshotStore) updateLastBlock(num uint64) {
	atomic.StoreUint64(&s.lastNumber, num)
}

// deleteLower deletes snapshots that have a block number lower than the passed in parameter
func (s *snapshotStore) deleteLower(num uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()

	i := sort.Search(len(s.list), func(i int) bool {
		return s.list[i].Number >= num
	})
	s.list = s.list[i:]
}

// find returns the index of the first closest snapshot to the number specified
func (s *snapshotStore) find(num uint64) *Snapshot {
	s.lock.Lock()
	defer s.lock.Unlock()

	if len(s.list) == 0 {
		return nil
	}

	// fast track, check the last item
	if last := s.list[len(s.list)-1]; last.Number < num {
		return last
	}

	i := sort.Search(len(s.list), func(i int) bool {
		return s.list[i].Number >= num
	})

	if i < len(s.list) {
		if i == 0 {
			return s.list[0]
		}

		if s.list[i].Number == num {
			return s.list[i]
		}

		return s.list[i-1]
	}

	return nil
}

// add adds a new snapshot to the snapshot store
func (s *snapshotStore) add(snap *Snapshot) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// append and sort the list
	s.list = append(s.list, snap)
	sort.Sort(&s.list)
}

func (s *snapshotStore) replace(snap *Snapshot) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for i, sn := range s.list {
		if sn.Number == snap.Number {
			s.list[i] = snap

			return
		}
	}
}

// snapshotSortedList defines the sorted snapshot list
type snapshotSortedList []*Snapshot

// Len returns the size of the sorted snapshot list
func (s snapshotSortedList) Len() int {
	return len(s)
}

// Swap swaps two values in the sorted snapshot list
func (s snapshotSortedList) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Less checks if the element at index I has a lower number than the element at index J
func (s snapshotSortedList) Less(i, j int) bool {
	return s[i].Number < s[j].Number
}

// readDataStore attempts to read the specific file from file storage
func readDataStore(path string, obj interface{}) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, obj); err != nil {
		return err
	}

	return nil
}

// writeDataStore attempts to write the specific file to file storage
func writeDataStore(path string, obj interface{}) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	//nolint: gosec
	if err := ioutil.WriteFile(path, data, 0755); err != nil {
		return err
	}

	return nil
}
