package tpke

import (
	cryptoRand "crypto/rand"
	"math"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	bls "github.com/kilic/bls12-381"
)

func TestSingleSignature(t *testing.T) {
	s := rand.NewSource(time.Now().UnixNano())
	r := rand.New(s)
	fr, _ := bls.NewFr().Rand(r)
	g1 := bls.NewG1()
	sk := &PrivateKey{
		fr: fr,
	}
	pk := &PublicKey{
		pg1: g1.MulScalar(g1.New(), &bls.G1One, fr),
	}

	msg := []byte("pizza pizza pizza pizza pizza pizza pizza pizza pizza pizza pizza pizza pizza")
	share := sk.SignShare(msg)
	if !pk.VerifySigShare(msg, share) {
		t.Fatalf("invalid signature")
	}
}

func TestThresholdSignature(t *testing.T) {
	nEnv := os.Getenv("N")

	size, err := strconv.Atoi(nEnv)
	if err != nil || size <= 0 {
		size = 3
	}

	threshold := int(math.Floor(float64(size)/2) + 1)

	t.Logf(">>>> n = %d, t = %d", size, threshold)

	dkgElapsed, sks, pk, scaler := dkg(size, threshold)
	t.Log("DKG took", dkgElapsed)

	// for i := 0; i < loop; i++ {
	// 	signElapsed, aggregateElapsed, sig, _, _, _, _, err := signAndAggregate(sks, pk, threshold, scaler)

	// 	if err != nil {
	// 		t.Fatalf(err.Error())
	// 	}
	// 	if sig == nil {
	// 		t.Fatalf("invalid signature")
	// 	}
	// 	totalSignTime += signElapsed
	// 	totalAggregateTime += aggregateElapsed

	// 	isValid, sigs := Verify(pk, msg, threshold, inputs, scaler, matrix, shares)

	// 	if !isValid || sigs == nil {
	// 		t.Fatalf("invalid signature")
	// 	}

	// 	s0 := sigs[0]
	// 	for i := 1; i < len(sigs); i++ {
	// 		if !sigs[i].Equals(s0) {
	// 			t.Fatalf("different signature")
	// 		}
	// 	}
	// }

	loop := 100
	totalSignTime := time.Duration(0)
	totalAggregateTime := time.Duration(0)

	for i := 0; i < loop; i++ {
		signElapsed, aggregateElapsed, sig, _, _, _, _, err := signAndAggregate(sks, pk, threshold, scaler)

		if err != nil {
			t.Fatalf(err.Error())
		}
		if sig == nil {
			t.Fatalf("invalid signature")
		}

		totalSignTime += signElapsed
		totalAggregateTime += aggregateElapsed
	}

	t.Log("Sign took", totalSignTime/time.Duration(loop))
	t.Log("Aggregate took", totalAggregateTime/time.Duration(loop))
}

func dkg(n int, t int) (time.Duration, map[int]*PrivateKey, *PublicKey, int) {
	dkgStart := time.Now()

	dkg := NewDKG(n, t)
	dkg.Prepare()
	if err := dkg.Verify(); err != nil {
		panic(err)
	}
	sks := dkg.GetPrivateKeys()
	pk := dkg.PublishGlobalPublicKey()
	scaler := dkg.GetScaler()

	dkgElapsed := time.Since(dkgStart)
	return dkgElapsed, sks, pk, scaler
}

func generateRandomMsg(length int) ([]byte, error) {
	randomBytes := make([]byte, length)

	_, err := cryptoRand.Read(randomBytes)
	if err != nil {
		return nil, err
	}

	return randomBytes, nil
}

func signAndAggregate(sks map[int]*PrivateKey, pk *PublicKey, threshold int, scalar int) (time.Duration, time.Duration, *Signature, [][]int, []*SignatureShare, map[int]*SignatureShare, []byte, error) {
	// sign
	signStart := time.Now()
	// msg := []byte("pizza pizza pizza pizza pizza pizza pizza pizza pizza pizza pizza pizza pizza")
	msg, err := generateRandomMsg(50)
	if err != nil {
		return 0, 0, nil, nil, nil, nil, nil, err
	}
	inputs := make(map[int]*SignatureShare, len(sks))

	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(sks))

	for i, sk := range sks {
		go func(i int, sk *PrivateKey) {
			defer wg.Done()
			share := sk.SignShare(msg)
			mu.Lock()
			inputs[i] = share
			mu.Unlock()
		}(i, sk)
	}
	wg.Wait()

	signElapsed := time.Since(signStart)

	// aggregate
	aggregateStart := time.Now()
	sig, matrix, shares, err := Aggregate(pk, msg, threshold, inputs, scalar)
	if err != nil {
		return 0, 0, nil, nil, nil, inputs, msg, err
	}

	aggregateElapsed := time.Since(aggregateStart)

	return signElapsed, aggregateElapsed, sig, matrix, shares, inputs, msg, nil
}
