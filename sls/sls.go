package sls

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"eb-github.com/ed-silva/generate-secure-pillar/pki"
	yaml "github.com/esilva-everbridge/yaml"
	"github.com/gosexy/to"
	"github.com/sirupsen/logrus"
	yamlv1 "gopkg.in/yaml.v1"
)

// pgpHeader header const
const pgpHeader = "-----BEGIN PGP MESSAGE-----"
const encrypt = "encrypt"
const decrypt = "decrypt"

var logger *logrus.Logger
var p pki.Pki

// Sls sls data
type Sls struct {
	SecretNames     []string
	SecretValues    []string
	TopLevelElement string
	PublicKeyRing   string
	SecretKeyRing   string
	PgpKeyName      string
	Yaml            *yaml.Yaml
}

// New return Sls struct
func New(secretNames []string, secretValues []string, topLevelElement string, publicKeyRing string, secretKeyRing string, pgpKeyName string, log *logrus.Logger) Sls {
	if log != nil {
		logger = log
	} else {
		logger = logrus.New()
	}

	s := Sls{secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName, yaml.New()}
	p = pki.New(pgpKeyName, publicKeyRing, secretKeyRing, logger)

	return s
}

// ReadSlsFile open and read a yaml file, if the file has include statements
// we throw an error as the YAML parser will try to act on the include directives
func (s *Sls) ReadSlsFile(filePath string) error {
	fullPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return err
	}

	// Splits on newlines by default.
	scanner := bufio.NewScanner(f)

	// https://golang.org/pkg/bufio/#Scanner.Scan
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "include:") {
			return fmt.Errorf("skipping %s: contains include directives", filePath)
		}
	}
	if err = scanner.Err(); err != nil {
		return err
	}

	err = s.Yaml.Read(fullPath)
	return err
}

// WriteSlsFile writes a buffer to the specified file
// If the outFilePath is not stdout an INFO string will be printed to stdout
func (s *Sls) WriteSlsFile(buffer bytes.Buffer, outFilePath string) {
	fullPath, err := filepath.Abs(outFilePath)
	if err != nil {
		fullPath = outFilePath
	}

	stdOut := false
	if fullPath == os.Stdout.Name() {
		stdOut = true
	}

	err = ioutil.WriteFile(fullPath, buffer.Bytes(), 0644)
	if err != nil {
		logger.Fatal("error writing sls file: ", err)
	}
	if !stdOut {
		logger.Printf("Wrote out to file: '%s'", outFilePath)
	}
}

// FindSlsFiles recurses through the given searchDir returning a list of .sls files and it's length
func (s *Sls) FindSlsFiles(searchDir string) ([]string, int) {
	searchDir, err := filepath.Abs(searchDir)
	if err != nil {
		logger.Fatal(err)
	}
	fileList := []string{}
	err = filepath.Walk(searchDir, func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() && strings.Contains(f.Name(), ".sls") {
			fileList = append(fileList, path)
		}
		return nil
	})
	if err != nil {
		logger.Fatal("error walking file path: ", err)
	}

	return fileList, len(fileList)
}

// CipherTextYamlBuffer returns a buffer with encrypted and formatted yaml text
// If the 'all' flag is set all values under the designated top level element are encrypted
func (s *Sls) CipherTextYamlBuffer(filePath string) (bytes.Buffer, error) {
	var buffer bytes.Buffer
	err := s.CheckForFile(filePath)
	if err != nil {
		return buffer, err
	}
	filePath, err = filepath.Abs(filePath)
	if err != nil {
		return buffer, err
	}

	err = s.ReadSlsFile(filePath)
	if err != nil {
		return buffer, err
	}

	buffer = s.PerformAction(encrypt)
	return buffer, err
}

// PlainTextYamlBuffer decrypts all values under the top level element and returns a formatted buffer
func (s *Sls) PlainTextYamlBuffer(filePath string) (bytes.Buffer, error) {
	var buffer bytes.Buffer
	err := s.CheckForFile(filePath)
	if err != nil {
		return buffer, err
	}
	filePath, err = filepath.Abs(filePath)
	if err != nil {
		return buffer, err
	}

	err = s.ReadSlsFile(filePath)
	if err != nil {
		return buffer, err
	}

	buffer = s.PerformAction(decrypt)
	return buffer, err
}

// FormatBuffer returns a formatted .sls buffer with the gpg renderer line
func (s *Sls) FormatBuffer() bytes.Buffer {
	var buffer bytes.Buffer

	out, err := yamlv1.Marshal(s.Yaml.Values)
	if err != nil {
		logger.Fatal(err)
	}

	buffer.WriteString("#!yaml|gpg\n\n")
	buffer.WriteString(string(out))

	return buffer
}

// CheckForFile does exactly what it says on the tin
func (s *Sls) CheckForFile(filePath string) error {
	fi, err := os.Stat(filePath)
	if err != nil {
		logger.Fatalf("cannot stat %s: %s", filePath, err)
	}
	switch mode := fi.Mode(); {
	case mode.IsRegular():
		return nil
	case mode.IsDir():
		logger.Fatalf("%s is a directory", filePath)
	}

	return err
}

// ProcessYaml encrypts elements matching keys specified on the command line
func (s *Sls) ProcessYaml() {
	for index := 0; index < len(s.SecretNames); index++ {
		cipherText := ""
		if index >= 0 && index < len(s.SecretValues) {
			cipherText = p.EncryptSecret(s.SecretValues[index])
		}
		err := s.SetValueFromPath(s.SecretNames[index], cipherText)
		if err != nil {
			logger.Fatalf("error setting value: %s", err)
		}
	}
}

// ProcessDir will recursively apply findSlsFiles
// It will either encrypt or decrypt, as specified by the action flag
// It writes replaces the files found
func (s *Sls) ProcessDir(recurseDir string, action string) {
	info, err := os.Stat(recurseDir)
	if err != nil {
		logger.Fatalf("cannot stat %s: %s", recurseDir, err)
	}
	if info.IsDir() {
		slsFiles, count := s.FindSlsFiles(recurseDir)
		if count == 0 {
			logger.Fatalf("%s has no sls files", recurseDir)
		}
		for _, file := range slsFiles {
			logger.Infof("processing %s", file)
			var buffer bytes.Buffer
			if action == encrypt {
				buffer, err = s.CipherTextYamlBuffer(file)
				if err != nil {
					logger.Warnf("%s", err)
					continue
				}
			} else if action == decrypt {
				buffer, err = s.PlainTextYamlBuffer(file)
				if err != nil {
					logger.Warnf("%s", err)
					continue
				}
			} else {
				logger.Fatalf("unknown action: %s", action)
			}
			s.WriteSlsFile(buffer, file)
		}
	} else {
		logger.Fatalf("%s is not a directory", recurseDir)
	}
}

// GetValueFromPath returns the value from a path string
func (s *Sls) GetValueFromPath(path string) interface{} {
	parts := strings.Split(path, ":")

	args := make([]interface{}, len(parts))
	for i := 0; i < len(parts); i++ {
		args[i] = parts[i]
	}
	results := s.Yaml.Get(args...)
	return results
}

// SetValueFromPath returns the value from a path string
func (s *Sls) SetValueFromPath(path string, value string) error {
	parts := strings.Split(path, ":")

	// construct the args list
	args := make([]interface{}, len(parts)+1)
	for i := 0; i < len(parts); i++ {
		args[i] = parts[i]
	}
	args[len(args)-1] = value
	err := s.Yaml.Set(args...)
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", err)
}

// PerformAction takes an action string (encrypt or decrypt)
// and applies that action on all items
func (s *Sls) PerformAction(action string) bytes.Buffer {

	if action == encrypt || action == decrypt {
		var stuff = make(map[string]interface{})

		for key := range s.Yaml.Values {
			if s.TopLevelElement != "" {
				vals := s.GetValueFromPath(key)
				if s.TopLevelElement == key {
					stuff[key] = processValues(vals, action)
				} else {
					stuff[key] = vals
				}
			} else {
				vals := s.GetValueFromPath(key)
				stuff[key] = processValues(vals, action)
			}
		}
		// replace the values in the Yaml object
		s.Yaml.Values = stuff
	}

	return s.FormatBuffer()
}

func processValues(vals interface{}, action string) interface{} {
	vtype := reflect.TypeOf(vals).Kind()

	var res interface{}
	switch vtype {
	case reflect.Slice:
		res = doSlice(vals, action)
	case reflect.Map:
		res = doMap(vals.(map[interface{}]interface{}), action)
	case reflect.String:
		switch action {
		case decrypt:
			if isEncrypted(to.String(vals)) {
				plainText, err := p.DecryptSecret(to.String(vals))
				if err != nil {
					logger.Errorf("error decrypting value: %s", err)
				} else {
					vals = plainText
				}
			}
		case encrypt:
			if !isEncrypted(to.String(vals)) {
				vals = p.EncryptSecret(to.String(vals))
			}
		}
		res = to.String(vals)
	}

	return res
}

func doSlice(vals interface{}, action string) interface{} {
	var things []interface{}

	for _, item := range vals.([]interface{}) {
		var thing interface{}
		vtype := reflect.TypeOf(item).Kind()

		switch vtype {
		case reflect.Slice:
			things = append(things, doSlice(item, action))
		case reflect.Map:
			thing = item
			things = append(things, doMap(thing.(map[interface{}]interface{}), action))
		case reflect.String:
			switch action {
			case decrypt:
				if isEncrypted(to.String(item)) {
					plainText, err := p.DecryptSecret(to.String(item))
					if err != nil {
						logger.Errorf("error decrypting value: %s", err)
						thing = to.String(item)
					} else {
						thing = plainText
					}
				}
			case encrypt:
				if !isEncrypted(to.String(item)) {
					thing = p.EncryptSecret(to.String(item))
				}
			}
			things = append(things, thing)
		}
	}

	return things
}

func doMap(vals map[interface{}]interface{}, action string) map[interface{}]interface{} {
	var ret = make(map[interface{}]interface{})

	for key, val := range vals {
		vtype := reflect.TypeOf(val).Kind()

		switch vtype {
		case reflect.Slice:
			ret[key] = doSlice(val, action)
		case reflect.Map:
			ret[key] = doMap(val.(map[interface{}]interface{}), action)
		case reflect.String:
			switch action {
			case decrypt:
				if isEncrypted(to.String(val)) {
					plainText, err := p.DecryptSecret(to.String(val))
					if err != nil {
						logger.Errorf("error decrypting value for: %s, %s", key, err)
					} else {
						val = plainText
					}
				}
			case encrypt:
				if !isEncrypted(to.String(val)) {
					val = p.EncryptSecret(to.String(val))
				}
			}
			ret[key] = val
		}
	}

	return ret
}

func isEncrypted(str string) bool {
	return strings.Contains(str, pgpHeader)
}
