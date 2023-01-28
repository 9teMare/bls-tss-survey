/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binance_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	. "github.ibm.com/fabric-security-research/tss/threshold"
	. "github.ibm.com/fabric-security-research/tss/types"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	mpc "github.ibm.com/fabric-security-research/tss/mpc/binance/ecdsa"
	comm "github.ibm.com/fabric-security-research/tss/net"
	"github.ibm.com/fabric-security-research/tss/testutil/tlsgen"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestThresholdBinanceECDSA(t *testing.T) {
	n := 4

	var members []uint16
	for i := 1; i <= n; i++ {
		members = append(members, uint16(i))
	}

	ca, err := tlsgen.NewCA()
	assert.NoError(t, err)

	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(ca.CertBytes())

	tlsCert, err := ca.NewServerCertKeyPair("127.0.0.1")
	assert.NoError(t, err)

	var commParties []*comm.Party
	var signers []*tlsgen.CertKeyPair
	var loggers []*commLogger
	var listeners []net.Listener
	var stopFuncs []func()

	membership := make(map[UniversalID]PartyID)

	for id := 1; id <= n; id++ {
		l := logger(id, t.Name())
		loggers = append(loggers, l)

		s := newSigner(ca, t)
		signers = append(signers, s)

		lsnr := comm.Listen("127.0.0.1:0", tlsCert.Cert, tlsCert.Key)
		listeners = append(listeners, lsnr)

		commParties = append(commParties, &comm.Party{
			Logger:   l,
			Address:  lsnr.Addr().String(),
			Identity: s.Cert,
		})

		membership[UniversalID(id)] = PartyID(id)
	}

	membershipFunc := func() map[UniversalID]PartyID {
		return membership
	}

	var parties []*Scheme

	for id := 1; id <= n; id++ {
		stop, s := createParty(id, signers[id-1], n, certPool, listeners, loggers, commParties, membershipFunc)
		parties = append(parties, s)
		stopFuncs = append(stopFuncs, stop)
	}

	defer func() {
		for _, stop := range stopFuncs {
			stop()
		}
	}()

	var wg sync.WaitGroup
	wg.Add(len(parties))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	start := time.Now()
	for _, p := range parties {
		go func(p *Scheme) {
			defer wg.Done()
			data, err := p.KeyGen(ctx, len(parties), len(parties)-1)
			p.StoredData = data
			assert.NoError(t, err)
			assert.NotNil(t, data)
		}(p)
	}

	wg.Wait()
	cancel()

	elapsed := time.Since(start)
	t.Log("DKG elapsed", elapsed)

	t.Log("Running signing")

	k := 1
	concurrentWg := sync.WaitGroup{}
	concurrentWg.Add(k)

	for i := 0; i < k; i++ {
		// Sleep to simulate parties starting at different times
		time.Sleep(time.Millisecond * 50)
		go func(i int) {
			defer concurrentWg.Done()
			thresholdSign(t, i, parties, k)
		}(i)
	}

	concurrentWg.Wait()
}

func thresholdSign(t *testing.T, i int, parties []*Scheme, k int) {
	msg := fmt.Sprintf("msg %d", i)
	topic := fmt.Sprintf("topic %d", i)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(k)*10)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(len(parties))

	start := time.Now()

	for _, p := range parties {
		// Sleep to simulate parties starting at different times
		time.Sleep(time.Millisecond * 50)
		go func(p *Scheme) {
			defer wg.Done()
			signature, err := p.Sign(ctx, sha256Digest([]byte(msg)), topic)
			require.NoError(t, err)
			assert.NotEmpty(t, signature)

			pkBytes, err := p.ThresholdPK()
			assert.NoError(t, err)

			pk, err := x509.ParsePKIXPublicKey(pkBytes)
			assert.NoError(t, err)

			assert.True(t, ecdsa.VerifyASN1(pk.(*ecdsa.PublicKey), sha256Digest([]byte(msg)), signature))
		}(p)
	}

	wg.Wait()

	elapsed := time.Since(start)
	t.Log("Signing elapsed", elapsed)
}

func createParty(id int, signer *tlsgen.CertKeyPair, n int, certPool *x509.CertPool, listeners []net.Listener, loggers []*commLogger, commParties []*comm.Party, membershipFunc func() map[UniversalID]PartyID) (func(), *Scheme) {
	remoteParties := make(comm.SocketRemoteParties)

	auth := func(tlsContext []byte) comm.Handshake {
		h := comm.Handshake{
			TLSBinding: tlsContext,
			Identity:   signer.Cert,
			Timestamp:  time.Now().Unix(),
		}

		sig, err := signer.Sign(rand.Reader, sha256Digest(h.Bytes()), nil)
		if err != nil {
			panic("failed signing")
		}

		h.Signature = sig

		return h
	}

	for i := 0; i < n; i++ {
		if i+1 == id {
			continue
		}

		remoteParties[i+1] = comm.NewSocketRemoteParty(comm.PartyConnectionConfig{
			AuthFunc: auth,
			TlsCAs:   certPool,
			Id:       i + 1,
			Endpoint: listeners[i].Addr().String(),
		}, loggers[id-1])

	}

	commParties[id-1].SendMessage = remoteParties.Send

	p2id := make(map[string]uint16)
	for i, p := range commParties {
		p2id[hex.EncodeToString(sha256Digest(p.Identity))] = uint16(i + 1)
	}

	in, stop := comm.ServiceConnections(listeners[id-1], p2id, loggers[id-1])
	commParties[id-1].InMessages = in

	kgf := func(id uint16) KeyGenerator {
		return mpc.NewParty(id, loggers[id-1])
	}

	sf := func(id uint16) Signer {
		return mpc.NewParty(id, loggers[id-1])
	}

	s := DefaultScheme(uint16(id), loggers[id-1], kgf, sf, len(commParties)-1, remoteParties.Send, membershipFunc)

	go func(in <-chan comm.InMsg) {
		for msg := range in {
			inMsg := &IncMessage{
				MsgType: msg.Type,
				Data:    msg.Data,
				Topic:   msg.Topic,
				Source:  msg.From,
			}

			s.HandleMessage(inMsg)
		}
	}(in)
	return stop, s
}

func logger(id int, testName string) *commLogger {
	logConfig := zap.NewDevelopmentConfig()
	baseLogger, _ := logConfig.Build()
	logger := baseLogger.With(zap.String("t", testName)).With(zap.String("id", fmt.Sprintf("%d", id)))
	return &commLogger{Logger: &loggerWithDebug{SugaredLogger: logger.Sugar()}, conf: &logConfig}
}

type loggerWithDebug struct {
	*zap.SugaredLogger
}

func (lwd *loggerWithDebug) DebugEnabled() bool {
	return false
}

type commLogger struct {
	conf *zap.Config
	Logger
}

func (l *commLogger) mute() {
	l.conf.Level.SetLevel(zapcore.WarnLevel)
}

func (*commLogger) DebugEnabled() bool {
	return false
}

func newSigner(ca tlsgen.CA, t *testing.T) *tlsgen.CertKeyPair {
	clientPair, err := ca.NewClientCertKeyPair()
	assert.NoError(t, err, "failed to create client key pair")

	return clientPair
}

func sha256Digest(b ...[]byte) []byte {
	hash := sha256.New()
	for _, bytes := range b {
		hash.Write(bytes)
	}
	return hash.Sum(nil)
}
