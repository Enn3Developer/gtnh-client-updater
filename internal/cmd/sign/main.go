// Command sign manages the gtnh-update release signing key.
//
//	go run ./internal/cmd/sign keygen -priv <file>
//	    Creates a key pair: the private key goes to <file> (mode 0600, never printed),
//	    the public key to internal/selfupdate/signing_key.pub (embedded in binaries).
//	go run ./internal/cmd/sign sign <checksums.txt>
//	    Signs with the private key from $RELEASE_SIGNING_KEY into <checksums.txt>.sig,
//	    after checking that the key matches the embedded public key.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Enn3Developer/gtnh-client-updater/internal/selfupdate"
)

const pubFile = "internal/selfupdate/signing_key.pub"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sign keygen -priv <file> | sign sign <checksums.txt>")
	}
	switch args[0] {
	case "keygen":
		fs := flag.NewFlagSet("keygen", flag.ExitOnError)
		priv := fs.String("priv", "", "where to write the private key (must not exist)")
		fs.Parse(args[1:])
		if *priv == "" {
			return errors.New("keygen needs -priv <file>")
		}
		return keygen(*priv)
	case "sign":
		if len(args) != 2 {
			return errors.New("usage: sign sign <checksums.txt>")
		}
		return sign(args[1])
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func keygen(privPath string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(privPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("private key file: %w", err)
	}
	if _, err := f.WriteString(selfupdate.EncodePrivateKey(priv)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(pubFile, []byte(selfupdate.EncodePublicKey(pub)), 0o644); err != nil {
		return err
	}
	fmt.Printf("private key written to %s (keep a backup somewhere safe)\npublic key written to %s\n", privPath, pubFile)
	return nil
}

func sign(sumsPath string) error {
	env := os.Getenv("RELEASE_SIGNING_KEY")
	if env == "" {
		return errors.New("RELEASE_SIGNING_KEY is not set")
	}
	priv, err := selfupdate.ParsePrivateKey(env)
	if err != nil {
		return err
	}
	pubData, err := os.ReadFile(pubFile)
	if err != nil {
		return err
	}
	pub, err := selfupdate.ParsePublicKey(string(pubData))
	if err != nil {
		return fmt.Errorf("%s: %w", pubFile, err)
	}
	if !pub.Equal(priv.Public()) {
		return errors.New("RELEASE_SIGNING_KEY does not match the embedded public key -- binaries would reject this release")
	}
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	sig := selfupdate.SignChecksums(priv, sums)
	if err := selfupdate.VerifyChecksums(pub, sums, []byte(sig)); err != nil {
		return err
	}
	if err := os.WriteFile(sumsPath+".sig", []byte(sig), 0o644); err != nil {
		return err
	}
	fmt.Println("signed", sumsPath)
	return nil
}
