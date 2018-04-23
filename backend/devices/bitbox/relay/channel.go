package relay

import (
	"crypto/rand"
	"encoding/json"
	"reflect"
	"time"

	"github.com/btcsuite/btcutil/base58"

	"github.com/shiftdevices/godbb/util/config"
	"github.com/shiftdevices/godbb/util/errp"
)

// Channel implements an encrypted communication channel between the desktop and the paired mobile.
type Channel struct {
	// ChannelID is the identifier which uniquely identifies the channel between the parties.
	ChannelID string `json:"id"`

	// EncryptionKey is used to encrypt the communication between the desktop and the mobile.
	EncryptionKey []byte `json:"key"`
}

// NewChannel returns a new channel with the given channel identifier and encryption key.
func NewChannel(channelID string, encryptionKey []byte) *Channel {
	return &Channel{
		ChannelID:     channelID,
		EncryptionKey: encryptionKey,
	}
}

// NewChannelWithRandomKey returns a new channel with a random encryption key and identifier.
func NewChannelWithRandomKey() *Channel {
	channelID := make([]byte, 32)
	if _, err := rand.Read(channelID); err != nil {
		panic(err)
	}

	encryptionKey := make([]byte, 32)
	if _, err := rand.Read(encryptionKey); err != nil {
		panic(err)
	}

	// The channel identifier may not contain '=' and thus it cannot be encoded with base64.
	return NewChannel(base58.Encode(channelID), encryptionKey)
}

// NewChannelFromConfigFile returns a new channel with the channel identifier and encryption key
// from the config file or nil if the config file does not exist.
func NewChannelFromConfigFile() *Channel {
	configFile := config.NewFile(configFileName)
	if configFile.Exists() {
		var configuration configuration
		if err := configFile.ReadJSON(&configuration); err != nil {
			return nil
		}
		return configuration.channel()
	}
	return nil
}

// StoreToConfigFile stores the channel to the config file.
func (channel *Channel) StoreToConfigFile() error {
	configuration := newConfiguration(channel)
	configFile := config.NewFile(configFileName)
	return configFile.WriteJSON(configuration)
}

// relayServer returns the configured relay server.
// The server is hardcoded for now but can be loaded from the settings in the future.
func relayServer() Server {
	return DefaultServer
}

// waitForMessage waits for the given duration for the expected message from the paired mobile.
// Returns nil if the expected message was retrieved from the relay server and an error otherwise.
func (channel *Channel) waitForMessage(
	duration time.Duration,
	expectedMessage map[string]string,
) error {
	deadline := time.Now().Add(duration)
	for {
		message, err := PullOldestMessage(relayServer(), channel)
		if err != nil {
			return err
		}
		if message == nil {
			if time.Now().Before(deadline) {
				continue
			}
			return errp.New("Did not receive a response from the mobile in the given duration.")
		}
		var receivedMessage map[string]string
		err = json.Unmarshal(message, &receivedMessage)
		if err != nil || !reflect.DeepEqual(expectedMessage, receivedMessage) {
			return errp.New("Received a different message from the paired mobile than expected.")
		}
		return nil
	}
}

// waitForValue waits for the given duration for the value with the given name from the mobile.
// Returns an error if no value with the given name has been received in the given duration.
func (channel *Channel) waitForValue(duration time.Duration, name string) (string, error) {
	deadline := time.Now().Add(duration)
	for {
		message, err := PullOldestMessage(relayServer(), channel)
		if err != nil {
			return "", err
		}
		if message == nil {
			if time.Now().Before(deadline) {
				continue
			}
			return "", errp.New("Did not receive a response from the mobile in the given duration.")
		}
		var object map[string]string
		err = json.Unmarshal(message, &object)
		var value, present = object[name]
		if err != nil || !present {
			return "", errp.Newf("Did not receive the value '%s' from the paired mobile.", name)
		}
		return value, nil
	}
}

// WaitForScanningSuccess waits for the given duration for the scanning success from the mobile.
// Returns nil if the scanning success was retrieved from the relay server and an error otherwise.
func (channel *Channel) WaitForScanningSuccess(duration time.Duration) error {
	return channel.waitForMessage(duration, map[string]string{"id": "success"})
}

// WaitForMobilePublicKey waits for the given duration for the ECDH public key from the mobile.
// Returns an error if no ECDH public key has been received from the server in the given duration.
func (channel *Channel) WaitForMobilePublicKey(duration time.Duration) (string, error) {
	return channel.waitForValue(duration, "ecdh")
}

// SendVerifyPass sends the verify pass from the BitBox to the paired mobile to finish pairing.
func (channel *Channel) SendVerifyPass(verifyPass interface{}) error {
	return PushMessage(relayServer(), channel, map[string]interface{}{
		"verifypass": verifyPass,
	})
}

// SendPairingTest sends the encrypted test string from the BitBox to the paired mobile.
func (channel *Channel) SendPairingTest(tfaTestString string) error {
	return PushMessage(relayServer(), channel, map[string]string{
		"tfa": tfaTestString,
	})
}

// action describes the JSON object for actions like ping, pong and clear.
type action struct {
	Action string `json:"action"`
}

// SendPing sends a 'ping' to the paired mobile to which it automatically responds with 'pong'.
func (channel *Channel) SendPing() error {
	return PushMessage(relayServer(), channel, &action{"ping"})
}

// WaitForPong waits for the given duration for the 'pong' from the mobile after sending 'ping'.
// Returns nil if the pong was retrieved from the relay server and an error otherwise.
func (channel *Channel) WaitForPong(duration time.Duration) error {
	return channel.waitForMessage(duration, map[string]string{"action": "pong"})
}

// SendClear clears the screen of the paired mobile.
func (channel *Channel) SendClear() error {
	return PushMessage(relayServer(), channel, &action{"clear"})
}

// SendXpubEcho sends the encrypted xpub echo from the BitBox to the paired mobile.
func (channel *Channel) SendXpubEcho(xpubEcho string) error {
	return PushMessage(relayServer(), channel, map[string]string{
		"echo": xpubEcho,
		"type": "p2pkh",
	})
}

// SendSigningEcho sends the encrypted signing echo from the BitBox to the paired mobile.
func (channel *Channel) SendSigningEcho(signingEcho string, transaction string) error {
	return PushMessage(relayServer(), channel, map[string]string{
		"echo": signingEcho,
		"tx":   transaction,
	})
}

// WaitForSigningPin waits for the given duration for the 2FA signing PIN from the mobile.
// Returns an error if no 2FA signing PIN was available on the relay server in the given duration.
// Otherwise, the returned value is either the PIN (on confirmation) or "abort" (on cancel).
func (channel *Channel) WaitForSigningPin(duration time.Duration) (string, error) {
	return channel.waitForValue(duration, "pin")
}

// SendRandomNumberEcho sends the encrypted random number echo from the BitBox to the paired mobile.
func (channel *Channel) SendRandomNumberEcho(randomNumberEcho string) error {
	return PushMessage(relayServer(), channel, map[string]string{
		"echo": randomNumberEcho,
	})
}

// WaitForRandomNumberClear waits for the given duration for a random number clear from the mobile.
// Returns nil if a random number clear was retrieved from the relay server and an error otherwise.
func (channel *Channel) WaitForRandomNumberClear(duration time.Duration) error {
	return channel.waitForMessage(duration, map[string]string{"random": "clear"})
}