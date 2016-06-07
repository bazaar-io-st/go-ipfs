package dht

import (
	"time"

	key "github.com/ipfs/go-ipfs/blocks/key"
	goprocess "gx/ipfs/QmQopLATEYMNg7dVqZRNDfeE2S1yKy8zrRh5xnYiuqeZBn/goprocess"
	goprocessctx "gx/ipfs/QmQopLATEYMNg7dVqZRNDfeE2S1yKy8zrRh5xnYiuqeZBn/goprocess/context"
	peer "gx/ipfs/QmbyvM8zRFDkbFdYyt1MnevUMJ62SiSGbfDFZ3Z8nkrzr4/go-libp2p-peer"
	multihash "gx/ipfs/QmYf7ng2hG5XBtJA3tN34DQ2GUN5HNksEw1rLDkmr6vGku/go-multihash"
	context "gx/ipfs/QmZy2y8t9zQH2a1b8q2ZSLKp17ATuJoCNxxyMFG5qFExpt/go-net/context"
	"encoding/hex"
)

const MAGIC string = "0000000000000000000000000000000000000000000000000000000000000000"

type ProviderManager struct {
	// all non channel fields are meant to be accessed only within
	// the run method
	providers map[key.Key]*providerSet
	local     map[key.Key]struct{}
	lpeer     peer.ID

	getlocal chan chan []key.Key
	newprovs chan *addProv
	getprovs chan *getProv
	period   time.Duration
	proc     goprocess.Process
	magicID  peer.ID
}

type providerSet struct {
	providers []peer.ID
	set       map[peer.ID]time.Time
}

type addProv struct {
	k   key.Key
	val peer.ID
}

type getProv struct {
	k    key.Key
	resp chan []peer.ID
}

func NewProviderManager(ctx context.Context, local peer.ID) *ProviderManager {
	pm := new(ProviderManager)
	pm.getprovs = make(chan *getProv)
	pm.newprovs = make(chan *addProv)
	pm.providers = make(map[key.Key]*providerSet)
	pm.getlocal = make(chan chan []key.Key)
	pm.local = make(map[key.Key]struct{})
	pm.proc = goprocessctx.WithContext(ctx)
	pm.proc.Go(func(p goprocess.Process) { pm.run() })
	mID, _ := getMagicID()
	pm.magicID = mID
	return pm
}

func (pm *ProviderManager) run() {
	tick := time.NewTicker(time.Hour)
	for {
		select {
		case np := <-pm.newprovs:
			if np.val == pm.lpeer {
				pm.local[np.k] = struct{}{}
			}
			provs, ok := pm.providers[np.k]
			if !ok {
				provs = newProviderSet()
				pm.providers[np.k] = provs
			}
			provs.Add(np.val)

		case gp := <-pm.getprovs:
			var parr []peer.ID
			provs, ok := pm.providers[gp.k]
			if ok {
				parr = provs.providers
			}

			gp.resp <- parr

		case lc := <-pm.getlocal:
			var keys []key.Key
			for k := range pm.local {
				keys = append(keys, k)
			}
			lc <- keys

		case <-tick.C:
			for _, provs := range pm.providers {
				var filtered []peer.ID
				for p, t := range provs.set {
					if time.Now().Sub(t) > time.Hour*24 && p != pm.magicID {
						delete(provs.set, p)
					} else if time.Now().Sub(t) > time.Hour*168 && p == pm.magicID {
						delete(provs.set, p)
					} else {
						filtered = append(filtered, p)
					}
				}
				provs.providers = filtered
			}

		case <-pm.proc.Closing():
			return
		}
	}
}

func (pm *ProviderManager) AddProvider(ctx context.Context, k key.Key, val peer.ID) {
	prov := &addProv{
		k:   k,
		val: val,
	}
	select {
	case pm.newprovs <- prov:
	case <-ctx.Done():
	}
}

func (pm *ProviderManager) GetProviders(ctx context.Context, k key.Key) []peer.ID {
	gp := &getProv{
		k:    k,
		resp: make(chan []peer.ID, 1), // buffered to prevent sender from blocking
	}
	select {
	case <-ctx.Done():
		return nil
	case pm.getprovs <- gp:
	}
	select {
	case <-ctx.Done():
		return nil
	case peers := <-gp.resp:
		return peers
	}
}

func (pm *ProviderManager) GetLocal() []key.Key {
	resp := make(chan []key.Key)
	pm.getlocal <- resp
	return <-resp
}

func newProviderSet() *providerSet {
	return &providerSet{
		set: make(map[peer.ID]time.Time),
	}
}

func (ps *providerSet) Add(p peer.ID) {
	_, found := ps.set[p]
	if !found {
		ps.providers = append(ps.providers, p)
	}

	ps.set[p] = time.Now()
}

func getMagicID() (peer.ID, error){
	magicBytes, err := hex.DecodeString(MAGIC)
	if err != nil {
		return "", err
	}
	h, err := multihash.Encode(magicBytes, multihash.SHA2_256)
	if err != nil {
		return "", err
	}
	id, err := peer.IDFromBytes(h)
	if err != nil {
		return "", err
	}
	return id, nil
}