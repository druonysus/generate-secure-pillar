package main

import (
	"bytes"
	"fmt"
	"os"
	"runtime"

	"github.com/Everbridge/generate-secure-pillar/sls"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var logger = logrus.New()

var inputFilePath string
var outputFilePath = os.Stdout.Name()
var pgpKeyName string
var publicKeyRing = ""
var secretKeyRing = ""
var recurseDir string
var secretNames cli.StringSlice
var secretValues cli.StringSlice
var topLevelElement string
var yamlPath string
var updateInPlace bool

var defaultPubRing = "~/.gnupg/pubring.gpg"
var defaultSecRing = "~/.gnupg/secring.gpg"

var inputFlag = cli.StringFlag{
	Name:        "file, f",
	Value:       os.Stdin.Name(),
	Usage:       "input file (defaults to STDIN)",
	Destination: &inputFilePath,
}

var outputFlag = cli.StringFlag{
	Name:        "outfile, o",
	Value:       os.Stdout.Name(),
	Usage:       "output file (defaults to STDOUT)",
	Destination: &outputFilePath,
}

var fileFlags = []cli.Flag{
	inputFlag,
	outputFlag,
}

var secNamesFlag = cli.StringSliceFlag{
	Name:  "name, n",
	Usage: "secret name(s)",
	Value: &secretNames,
}

var secValsFlag = cli.StringSliceFlag{
	Name:  "value, s",
	Usage: "secret value(s)",
	Value: &secretValues,
}

var updateFlag = cli.BoolFlag{
	Name:        "update, u",
	Usage:       "update the input file",
	Destination: &updateInPlace,
}

var dirFlag = cli.StringFlag{
	Name:        "dir, d",
	Usage:       "recurse over all .sls files in the given directory",
	Destination: &recurseDir,
}

var appFlags = []cli.Flag{
	cli.StringFlag{
		Name:        "pubring, pub",
		Value:       defaultPubRing,
		Usage:       "PGP public keyring",
		Destination: &publicKeyRing,
	},
	cli.StringFlag{
		Name:        "secring, sec",
		Value:       defaultSecRing,
		Usage:       "PGP private keyring",
		Destination: &secretKeyRing,
	},
	cli.StringFlag{
		Name:        "pgp_key, k",
		Usage:       "PGP key name, email, or ID to use for encryption",
		Destination: &pgpKeyName,
	},
	cli.StringFlag{
		Name:        "element, e",
		Usage:       "Name of the top level element under which encrypted key/value pairs are kept",
		Destination: &topLevelElement,
	},
}

var appHelp = fmt.Sprintf(`%s
	CAVEAT: YAML files with include statements are not handled properly, so we skip them.
	
	EXAMPLES:
	# create a new sls file
	$ generate-secure-pillar -k "Salt Master" create --name secret_name1 --value secret_value1 --name secret_name2 --value secret_value2 --outfile new.sls
	
	# add to the new file
	$ generate-secure-pillar -k "Salt Master" update --name new_secret_name --value new_secret_value --file new.sls
	
	# update an existing value
	$ generate-secure-pillar -k "Salt Master" update --name secret_name --value secret_value3 --file new.sls
	
	# encrypt all plain text values in a file
	$ generate-secure-pillar -k "Salt Master" encrypt all --file us1.sls --outfile us1.sls
	# or use --update flag
	$ generate-secure-pillar -k "Salt Master" encrypt all --file us1.sls --update
	
	# encrypt all plain text values in a file under the element 'secret_stuff'
	$ generate-secure-pillar -k "Salt Master" --element secret_stuff encrypt all --file us1.sls --outfile us1.sls
	
	# recurse through all sls files, encrypting all values
	$ generate-secure-pillar -k "Salt Master" encrypt recurse -d /path/to/pillar/secure/stuff
	
	# recurse through all sls files, decrypting all values (requires imported private key)
	$ generate-secure-pillar decrypt recurse -d /path/to/pillar/secure/stuff
	
	# decrypt a specific existing value (requires imported private key)
	$ generate-secure-pillar decrypt path --path "some:yaml:path" --file new.sls
	
	# decrypt all files and re-encrypt with given key (requires imported private key)
	$ generate-secure-pillar -k "New Salt Master Key" rotate -d /path/to/pillar/secure/stuff

	# show all PGP key IDs used in a file
	$ generate-secure-pillar keys all --file us1.sls

	# show all keys used in all files in a given directory
	$ generate-secure-pillar keys recurse -d /path/to/pillar/secure/stuff

	# show the PGP Key ID used for an element at a path in a file
	$ generate-secure-pillar keys path --path "some:yaml:path" --file new.sls

`, cli.AppHelpTemplate)

var appCommands = []cli.Command{
	{
		Name:    "create",
		Aliases: []string{"c"},
		Usage:   "create a new sls file",
		Action: func(c *cli.Context) error {
			s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
			s.ProcessYaml()
			buffer := s.FormatBuffer("")
			sls.WriteSlsFile(buffer, outputFilePath)
			return nil
		},
		Flags: []cli.Flag{
			outputFlag,
			secNamesFlag,
			secValsFlag,
		},
	},
	{
		Name:    "update",
		Aliases: []string{"u"},
		Usage:   "update the value of the given key in the given file",
		Action: func(c *cli.Context) error {
			if inputFilePath != os.Stdin.Name() {
				outputFilePath = inputFilePath
			}
			s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
			err := s.ReadSlsFile(inputFilePath)
			if err != nil {
				logger.Fatal(err)
			}
			s.ProcessYaml()
			buffer := s.FormatBuffer("")
			sls.WriteSlsFile(buffer, outputFilePath)
			return nil
		},
		Flags: []cli.Flag{
			inputFlag,
			secNamesFlag,
			secValsFlag,
		},
	},
	{
		Name:    "encrypt",
		Aliases: []string{"e"},
		Usage:   "perform encryption operations",
		Action: func(c *cli.Context) error {
			return cli.ShowCommandHelp(c, "")
		},
		Subcommands: []cli.Command{
			{
				Name: "all",
				Flags: []cli.Flag{
					inputFlag,
					outputFlag,
					updateFlag,
				},
				Action: func(c *cli.Context) error {
					s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
					if inputFilePath != os.Stdin.Name() && updateInPlace {
						outputFilePath = inputFilePath
					}
					buffer, err := s.CipherTextYamlBuffer(inputFilePath)
					safeWrite(buffer, err)
					return nil
				},
			},
			{
				Name: "recurse",
				Flags: []cli.Flag{
					dirFlag,
				},
				Action: func(c *cli.Context) error {
					s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
					s.ProcessDir(recurseDir, "encrypt")
					return nil
				},
			},
		},
	},
	{
		Name:    "decrypt",
		Aliases: []string{"d"},
		Usage:   "perform decryption operations",
		Flags:   fileFlags,
		Action: func(c *cli.Context) error {
			return cli.ShowCommandHelp(c, "")
		},
		Subcommands: []cli.Command{
			{
				Name: "all",
				Flags: []cli.Flag{
					inputFlag,
					outputFlag,
					updateFlag,
				},
				Action: func(c *cli.Context) error {
					s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
					if inputFilePath != os.Stdin.Name() && updateInPlace {
						outputFilePath = inputFilePath
					}
					buffer, err := s.PlainTextYamlBuffer(inputFilePath)
					safeWrite(buffer, err)
					return nil
				},
			},
			{
				Name: "recurse",
				Flags: []cli.Flag{
					dirFlag,
				},
				Action: func(c *cli.Context) error {
					s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
					s.ProcessDir(recurseDir, "decrypt")
					return nil
				},
			},
			{
				Name: "path",
				Flags: []cli.Flag{
					inputFlag,
					cli.StringFlag{
						Name:        "path, p",
						Usage:       "YAML path to decrypt",
						Destination: &yamlPath,
					},
				},
				Action: func(c *cli.Context) error {
					s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
					err := s.ReadSlsFile(inputFilePath)
					if err != nil {
						logger.Fatal(err)
					}
					pathAction(&s, yamlPath, "decrypt")

					return nil
				},
			},
		},
	},
	{
		Name:    "rotate",
		Aliases: []string{"r"},
		Usage:   "decrypt existing files and re-encrypt with a new key",
		Flags: []cli.Flag{
			dirFlag,
			cli.StringFlag{
				Name:        "infile, f",
				Usage:       "input file",
				Destination: &inputFilePath,
			},
		},
		Action: func(c *cli.Context) error {
			if inputFilePath != "" {
				s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
				limChan := make(chan bool, 1)
				s.RotateFile(inputFilePath, limChan)
				<-limChan
				close(limChan)
			} else {
				err := rotateFiles(recurseDir)
				if err != nil {
					logger.Fatalf("%s", err)
				}
			}
			return nil
		},
	},
	{
		Name:    "keys",
		Aliases: []string{"k"},
		Usage:   "show PGP key IDs used",
		Flags:   fileFlags,
		Action: func(c *cli.Context) error {
			return cli.ShowCommandHelp(c, "")
		},
		Subcommands: []cli.Command{
			{
				Name: "all",
				Flags: []cli.Flag{
					inputFlag,
					outputFlag,
				},
				Action: func(c *cli.Context) error {
					s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
					if inputFilePath != os.Stdin.Name() && updateInPlace {
						outputFilePath = inputFilePath
					}
					buffer, err := s.KeysForYamlBuffer(inputFilePath)
					if err != nil {
						logger.Fatal(err)
					}
					fmt.Printf("%s\n", buffer.String())
					return nil
				},
			},
			{
				Name: "recurse",
				Flags: []cli.Flag{
					dirFlag,
				},
				Action: func(c *cli.Context) error {
					s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
					s.ProcessDir(recurseDir, "validate")
					return nil
				},
			},
			{
				Name: "path",
				Flags: []cli.Flag{
					inputFlag,
					cli.StringFlag{
						Name:        "path, p",
						Usage:       "YAML path to examine",
						Destination: &yamlPath,
					},
				},
				Action: func(c *cli.Context) error {
					s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
					err := s.ReadSlsFile(inputFilePath)
					if err != nil {
						logger.Fatal(err)
					}
					pathAction(&s, yamlPath, "validate")

					return nil
				},
			},
		},
	},
}

func main() {
	app := cli.NewApp()
	app.Version = "1.0.318"
	app.Authors = []cli.Author{
		cli.Author{
			Name:  "Ed Silva",
			Email: "ed.silva@everbridge.com",
		},
	}

	cli.AppHelpTemplate = appHelp

	app.Copyright = "(c) 2018 Everbridge, Inc."
	app.Usage = "Create and update encrypted content or decrypt encrypted content."
	app.Flags = appFlags

	app.Commands = appCommands

	err := app.Run(os.Args)
	if err != nil {
		logger.Fatal(err)
	}
}

func safeWrite(buffer bytes.Buffer, err error) {
	if err != nil {
		logger.Fatalf("%s", err)
	} else {
		sls.WriteSlsFile(buffer, outputFilePath)
	}
}

func pathAction(s *sls.Sls, path string, action string) {
	vals := s.GetValueFromPath(path)
	if vals != nil {
		vals = s.ProcessValues(vals, action)
		fmt.Printf("%s: %s\n", path, vals)
	} else {
		logger.Warnf("unable to find path: '%s'", path)
	}
}

func processFiles(recurseDir string) int {
	var fileCount int
	slsFiles, count := sls.FindSlsFiles(recurseDir)
	if count == 0 {
		logger.Fatalf("%s has no sls files", recurseDir)
	}

	cores := runtime.GOMAXPROCS(0)
	limChan := make(chan bool, cores)

	for i := 0; i < cores; i++ {
		limChan <- true
	}

	for _, file := range slsFiles {
		<-limChan
		s := sls.New(secretNames, secretValues, topLevelElement, publicKeyRing, secretKeyRing, pgpKeyName)
		go s.RotateFile(file, limChan)
		fileCount++
	}
	close(limChan)

	return fileCount
}

func rotateFiles(recurseDir string) error {
	info, err := os.Stat(recurseDir)
	if err != nil {
		logger.Fatalf("cannot stat %s: %s", recurseDir, err)
	}
	if info.IsDir() && info.Name() != ".." {
		count := processFiles(recurseDir)
		logger.Infof("Finished processing %d files.\n", count)
	} else {
		logger.Fatalf("%s is not a directory", recurseDir)
	}

	return nil
}
