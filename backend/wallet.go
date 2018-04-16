package backend

import (
	"fmt"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/shiftdevices/godbb/coins/btc"
	"github.com/shiftdevices/godbb/coins/btc/addresses"
	"github.com/shiftdevices/godbb/coins/btc/electrum"
	"github.com/shiftdevices/godbb/util/errp"
	"github.com/sirupsen/logrus"
)

const (
	// dev server for now
	electrumServerBitcoinRegtest  = "127.0.0.1:52001"
	electrumServerBitcoinTestnet  = "dev.shiftcrypto.ch:51002"
	electrumServerBitcoinMainnet  = "dev.shiftcrypto.ch:50002"
	electrumServerLitecoinTestnet = "dev.shiftcrypto.ch:51004"
	electrumServerLitecoinMainnet = "dev.shiftcrypto.ch:50004"
)

// ConnectionError indicates an error when establishing a network connection.
type ConnectionError error

// Wallet wraps a wallet of a specific coin identified by Code.
type Wallet struct {
	Code   string        `json:"code"`
	Name   string        `json:"name"`
	Wallet btc.Interface `json:"-"`

	WalletDerivationPath  string `json:"keyPath"`
	BlockExplorerTxPrefix string `json:"blockExplorerTxPrefix"`

	net             *chaincfg.Params
	addressType     addresses.AddressType
	failureCallback func(err error)
	log             *logrus.Entry
}

func maybeConnectionError(err error) error {
	if _, ok := errp.Cause(err).(electrum.ConnectionError); ok {
		return ConnectionError(err)
	}
	return err
}

func (wallet *Wallet) init(backend *Backend) error {
	wallet.log = backend.log.WithFields(logrus.Fields{"coin": wallet.Code, "wallet-name": wallet.Name,
		"net": wallet.net.Name, "address-type": wallet.addressType})
	var electrumServer string
	tls := true
	switch wallet.Code {
	case "tbtc", "tbtc-p2wpkh-p2sh":
		electrumServer = electrumServerBitcoinTestnet
	case "rbtc", "rbtc-p2wpkh-p2sh":
		electrumServer = electrumServerBitcoinRegtest
		tls = false
	case "btc", "btc-p2wpkh-p2sh":
		electrumServer = electrumServerBitcoinMainnet
	case "tltc-p2wpkh-p2sh":
		electrumServer = electrumServerLitecoinTestnet
	case "ltc-p2wpkh-p2sh":
		electrumServer = electrumServerLitecoinMainnet
	default:
		wallet.log.Panic("Unknown coin")
		panic(fmt.Sprintf("unknown coin %s", wallet.Code))
	}
	wrappedFailureCallback := func(err error) {
		if err != nil {
			wallet.failureCallback(maybeConnectionError(err))
		}
	}
	electrumClient, err := electrum.NewElectrumClient(electrumServer, tls, wrappedFailureCallback, wallet.log)
	if err != nil {
		return maybeConnectionError(err)
	}
	keyStore, err := newRelativeKeyStore(backend.device, wallet.WalletDerivationPath, wallet.log)
	if err != nil {
		return err
	}
	wallet.Wallet, err = btc.NewWallet(
		wallet.net,
		backend.db.SubDB(fmt.Sprintf("%s-%s", wallet.Code, keyStore.XPub().String()), wallet.log),
		keyStore,
		electrumClient,
		wallet.addressType,
		func(event btc.Event) {
			if event == btc.EventStatusChanged && wallet.Wallet.Initialized() {
				wallet.log.WithField("wallet-sync-start", time.Since(backend.walletsSyncStart)).
					Debug("Wallet sync time")
			}
			backend.events <- WalletEvent{Type: "wallet", Code: wallet.Code, Data: string(event)}
		},
		wallet.log,
	)
	return err
}
