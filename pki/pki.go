package pki

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"time"

	"github.com/keybase/go-crypto/openpgp"
	"github.com/keybase/go-crypto/openpgp/armor"
	"github.com/sirupsen/logrus"
)

var logger *logrus.Logger

// Pki pki info
type Pki struct {
	PublicKeyRing string
	SecretKeyRing string
	PgpKeyName    string
	PublicKey     *openpgp.Entity
	PubRing       openpgp.EntityList
	SecRing       openpgp.EntityList
}

// New returns a pki object
func New(pgpKeyName string, publicKeyRing string, secretKeyRing string) Pki {
	var err error
	logger = logrus.New()

	p := Pki{publicKeyRing, secretKeyRing, pgpKeyName, nil, nil, nil}
	publicKeyRing, err = p.ExpandTilde(p.PublicKeyRing)
	if err != nil {
		logger.Fatal("cannot expand public key ring path: ", err)
	}
	p.PublicKeyRing = publicKeyRing

	secKeyRing, err := p.ExpandTilde(p.SecretKeyRing)
	if err != nil {
		logger.Fatal("cannot expand secret key ring path: ", err)
	}
	p.SecretKeyRing = secKeyRing

	p.setSecKeyRing()
	p.setPubKeyRing()

	p.PublicKey = p.GetKeyByID(p.PubRing, p.PgpKeyName)
	if p.PublicKey == nil {
		logger.Fatalf("unable to find key '%s' in %s", p.PgpKeyName, p.PublicKeyRing)
	}

	return p
}

func (p *Pki) setSecKeyRing() {
	secretKeyRing, err := p.ExpandTilde(p.SecretKeyRing)
	if err != nil {
		logger.Warnf("error reading secring: %s", err)
	}
	p.SecretKeyRing = secretKeyRing
	privringFile, err := os.Open(secretKeyRing)
	if err != nil {
		logger.Warnf("unable to open secring: %s", err)
	}
	privring, err := openpgp.ReadKeyRing(privringFile)
	if err != nil {
		logger.Warnf("cannot read private keys: %s", err)
	} else if privring == nil {
		logger.Warnf(fmt.Sprintf("%s is empty!", p.SecretKeyRing))
	} else {
		p.SecRing = privring
	}
	if err = privringFile.Close(); err != nil {
		logger.Fatal("error closing secring: ", err)
	}
}

func (p *Pki) setPubKeyRing() {
	publicKeyRing, err := p.ExpandTilde(p.PublicKeyRing)
	if err != nil {
		logger.Warnf("error reading pubring: %s", err)
	}
	p.PublicKeyRing = publicKeyRing
	pubringFile, err := os.Open(p.PublicKeyRing)
	if err != nil {
		logger.Fatal("cannot read public key ring: ", err)
	}
	pubring, err := openpgp.ReadKeyRing(pubringFile)
	if err != nil {
		logger.Fatal("cannot read public keys: ", err)
	}
	p.PubRing = pubring
	if err = pubringFile.Close(); err != nil {
		logger.Fatal("error closing pubring: ", err)
	}
}

// EncryptSecret returns encrypted plainText
func (p *Pki) EncryptSecret(plainText string) (cipherText string) {
	var memBuffer bytes.Buffer

	hints := openpgp.FileHints{IsBinary: false, ModTime: time.Time{}}
	writer := bufio.NewWriter(&memBuffer)
	w, err := armor.Encode(writer, "PGP MESSAGE", nil)
	if err != nil {
		logger.Fatal("Encode error: ", err)
	}

	plainFile, err := openpgp.Encrypt(w, []*openpgp.Entity{p.PublicKey}, nil, &hints, nil)
	if err != nil {
		logger.Fatal("Encryption error: ", err)
	}

	if _, err = fmt.Fprintf(plainFile, "%s", plainText); err != nil {
		logger.Fatal(err)
	}

	if err = plainFile.Close(); err != nil {
		logger.Fatal("unable to close file: ", err)
	}
	if err = w.Close(); err != nil {
		logger.Fatal(err)
	}
	if err = writer.Flush(); err != nil {
		logger.Fatal("error flusing writer: ", err)
	}

	return memBuffer.String()
}

// DecryptSecret returns decrypted cipherText
func (p *Pki) DecryptSecret(cipherText string) (plainText string, err error) {
	privringFile, err := os.Open(p.SecretKeyRing)
	if err != nil {
		return cipherText, fmt.Errorf("unable to open secring: %s", err)
	}
	privring, err := openpgp.ReadKeyRing(privringFile)
	if err != nil {
		return cipherText, fmt.Errorf("cannot read private keys: %s", err)
	} else if privring == nil {
		return cipherText, fmt.Errorf(fmt.Sprintf("%s is empty!", p.SecretKeyRing))
	}

	decbuf := bytes.NewBuffer([]byte(cipherText))
	block, err := armor.Decode(decbuf)
	if block.Type != "PGP MESSAGE" {
		return cipherText, fmt.Errorf("block type is not PGP MESSAGE: %s", err)
	}

	md, err := openpgp.ReadMessage(block.Body, privring, nil, nil)
	if err != nil {
		return cipherText, fmt.Errorf("unable to read PGP message: %s", err)
	}

	bytes, err := ioutil.ReadAll(md.UnverifiedBody)
	if err != nil {
		return cipherText, fmt.Errorf("unable to read message body: %s", err)
	}

	return string(bytes), err
}

// GetKeyByID returns a keyring by the given ID
func (p *Pki) GetKeyByID(keyring openpgp.EntityList, id interface{}) *openpgp.Entity {
	for _, entity := range keyring {

		idType := reflect.TypeOf(id).Kind()
		switch idType {
		case reflect.Uint64:
			if entity.PrimaryKey.KeyId == id.(uint64) {
				return entity
			} else if entity.PrivateKey.KeyId == id.(uint64) {
				return entity
			}
		case reflect.String:
			for _, ident := range entity.Identities {
				if ident.Name == id.(string) {
					return entity
				}
				if ident.UserId.Email == id.(string) {
					return entity
				}
				if ident.UserId.Name == id.(string) {
					return entity
				}
			}
		}
	}

	return nil
}

// ExpandTilde does exactly what it says on the tin
func (p *Pki) ExpandTilde(path string) (string, error) {
	if len(path) == 0 || path[0] != '~' {
		return path, nil
	}

	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(usr.HomeDir, path[1:]), nil
}

// KeyUsedForEncryptedFile gets the key used to encrypt a file
func (p *Pki) KeyUsedForEncryptedFile(file string) (string, error) {
	filePath, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}

	in, err := os.Open(filePath)
	if err != nil {
		return "", err
	}

	block, err := armor.Decode(in)
	if err != nil {
		return "", err
	}

	if block.Type != "PGP MESSAGE" {
		return "", fmt.Errorf("error decoding private key")
	}
	md, err := openpgp.ReadMessage(block.Body, p.SecRing, nil, nil)
	if err != nil {
		return "", fmt.Errorf("unable to read PGP message: %s", err)
	}

	for index := 0; index < len(md.EncryptedToKeyIds); index++ {
		id := md.EncryptedToKeyIds[index]
		keyStr := p.keyStringForID(id)
		if keyStr != "" {
			return keyStr, nil
		}
	}

	return "", fmt.Errorf("unable to find key for ids used")
}

func (p *Pki) keyStringForID(id uint64) string {
	keys := p.SecRing.KeysById(id, nil)
	if len(keys) > 0 {
		for n := 0; n < len(keys); n++ {
			key := keys[n]
			if key.Entity != nil {
				for k := range key.Entity.Identities {
					// return the first valid key
					return fmt.Sprintf("%X: %s\n", id, k)
				}
			}
		}
	}
	return ""
}
