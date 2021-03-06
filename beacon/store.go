package beacon

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"path"
	"sync"

	bolt "github.com/coreos/bbolt"
)

// store contains all the definitions and implementation of the logic that
// stores and loads beacon signatures. At the moment of writing, it consists of
// a boltdb key/value database store.

// Beacon holds the randomness as well as the info to verify it.
type Beacon struct {
	// PreviousRand is the previous randomness generated
	PreviousRand []byte
	// Round is the round number this beacon is tied to
	Round uint64
	// Randomness is the tbls signature of Round || PreviousRand
	Randomness []byte
}

// Message returns a slice of bytes as the message to sign or to verify
// alongside a beacon signature.
func Message(prevRand []byte, round uint64) []byte {
	var buff bytes.Buffer
	buff.Write(roundToBytes(round))
	buff.Write(prevRand)
	return buff.Bytes()
}

// Store is an interface to store Beacons packets where they can also be
// retrieved to be delivered to end clients.
type Store interface {
	Len() int
	Put(*Beacon) error
	Last() (*Beacon, error)
	Get(round uint64) (*Beacon, error)
	//Cursor() (*Cursor,error)
	// XXX Misses a delete function
}

// XXX To be implemented
type Cursor interface {
	Next() (*Beacon, error)
	Prev() (*Beacon, error)
	First() (*Beacon, error)
	Last() (*Beacon, error)
}

// boldStore implements the Store interface using the kv storage boltdb (native
// golang implementation). Internally, Beacons are stored as JSON-encoded in the
// db file.
type boltStore struct {
	sync.Mutex
	db  *bolt.DB
	len int
}

var bucketName = []byte("beacons")

const BoltFileName = "drand.db"

// NewBoltStore returns a Store implementation using the boltdb storage engine.
func NewBoltStore(folder string, opts *bolt.Options) (Store, error) {
	dbPath := path.Join(folder, BoltFileName)
	db, err := bolt.Open(dbPath, 0660, opts)
	if err != nil {
		return nil, err
	}

	// create the bucket already
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		return err
	})

	return &boltStore{
		db: db,
	}, err
}

func (b *boltStore) Len() int {
	return b.len
}

// Put implements the Store interface. WARNING: It does NOT verify that this
// beacon is not already saved in the database or not.
func (b *boltStore) Put(beacon *Beacon) error {
	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		key := roundToBytes(beacon.Round)
		buff, err := json.Marshal(beacon)
		if err != nil {
			return err
		}
		return bucket.Put(key, buff)
	})
	if err != nil {
		return err
	}
	b.Lock()
	b.len++
	b.Unlock()
	return nil
}

// Last returns the last beacon signature saved into the db
func (b *boltStore) Last() (*Beacon, error) {
	var beacon *Beacon
	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		cursor := bucket.Cursor()
		_, v := cursor.Last()
		if v == nil {
			return errors.New("no beacon saved yet")
		}
		b := &Beacon{}
		if err := json.Unmarshal(v, b); err != nil {
			return err
		}
		beacon = b
		return nil
	})
	return beacon, err
}

// Get returns the beacon saved at this round
func (b *boltStore) Get(round uint64) (*Beacon, error) {
	var beacon *Beacon
	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		v := bucket.Get(roundToBytes(round))
		if v == nil {
			return errors.New("no beacon saved for this round")
		}
		b := &Beacon{}
		if err := json.Unmarshal(v, b); err != nil {
			return err
		}
		beacon = b
		return nil
	})
	return beacon, err
}

type cbStore struct {
	Store
	cb func(*Beacon)
}

// NewCallbackStore returns a Store that calls the given callback in a goroutine
// each time a new Beacon is saved into the given store. It does not call the
// callback if there has been any errors while saving the beacon.
func NewCallbackStore(s Store, cb func(*Beacon)) Store {
	return &cbStore{Store: s, cb: cb}
}

func (c *cbStore) Put(b *Beacon) error {
	if err := c.Store.Put(b); err != nil {
		return err
	}
	go c.cb(b)
	return nil
}

func roundToBytes(r uint64) []byte {
	var buff bytes.Buffer
	binary.Write(&buff, binary.BigEndian, r)
	return buff.Bytes()
}
