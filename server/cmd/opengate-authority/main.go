// Command opengate-authority creates, replaces and stages the master keys from inside the
// cluster, printing roles and fingerprints and never key material.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/volchanskyi/opengate/server/internal/authority"
	"github.com/volchanskyi/opengate/server/internal/cert"
)

const usage = `Usage: opengate-authority <command> [flags]

  init                     create the issuing master key on a new cluster
  adopt -legacy-dir DIR    move the master key out of the secret it shared, once
  new-kit                  stage the next master key and its cross certificate
  layout -phase PHASE      write the role files for steady, changeover, retire, emergency or settle
  inspect                  print each role's certificates and fingerprints
  stage -from DIR -to DIR  copy the mounted role files into the server's folder, owner-only

Every command but stage takes -secret NAME, the secret holding the role files.
`

// defaultSecret is the secret the chart mounts into the server's start-up step.
const defaultSecret = "opengate-authority"

// sharedSecret holds the database passwords, and the master key until adopt moves it out.
const sharedSecret = "opengate-secrets"

// env is what a run reads and writes outside its arguments.
type env struct {
	out     io.Writer
	now     func() time.Time
	connect func() (*authority.Secrets, error)
}

func main() {
	e := env{out: os.Stdout, now: time.Now, connect: authority.InCluster}
	if err := e.run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "opengate-authority:", err)
		os.Exit(1)
	}
}

func (e env) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(e.out, usage)
		return errors.New("no command given")
	}
	command, rest := args[0], args[1:]
	switch command {
	case "stage":
		return e.stage(rest)
	case "init", "adopt", "new-kit", "layout", "inspect":
		return e.onSecret(ctx, command, rest)
	default:
		fmt.Fprint(e.out, usage)
		return fmt.Errorf("unknown command %q", command)
	}
}

func (e env) stage(args []string) error {
	fs := e.flags("stage")
	from := fs.String("from", "", "the mounted secret volume")
	to := fs.String("to", "", "the folder the server reads its role files from")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return errors.New("stage needs both -from and -to")
	}
	return authority.Stage(*from, *to)
}

// secretCommand is one run against the role files' secret.
type secretCommand struct {
	env
	secrets *authority.Secrets
	name    string
}

func (e env) onSecret(ctx context.Context, command string, args []string) error {
	fs := e.flags(command)
	name := fs.String("secret", defaultSecret, "the secret holding the role files")
	legacyDir := fs.String("legacy-dir", "", "adopt: the shared secret's master key, mounted as files")
	legacySecret := fs.String("legacy-secret", sharedSecret, "adopt: the secret the master key is moved out of")
	phase := fs.String("phase", "", "layout: steady, changeover, retire, emergency or settle")
	mps := fs.String("mps", "", `layout: "new" signs Intel AMT's certificate with the new key`)
	keepChain := fs.Bool("keep-chain", false, "layout settle: keep the cross certificate")
	dropOld := fs.Bool("drop-old", false, "layout settle: drop the old key entirely")
	if err := fs.Parse(args); err != nil {
		return err
	}
	secrets, err := e.connect()
	if err != nil {
		return err
	}
	c := secretCommand{env: e, secrets: secrets, name: *name}
	switch command {
	case "init":
		return c.change(ctx, func(current authority.Files) (authority.Files, error) {
			return authority.Init(current, e.now())
		})
	case "adopt":
		return c.adopt(ctx, *legacyDir, *legacySecret)
	case "new-kit":
		return c.change(ctx, func(current authority.Files) (authority.Files, error) {
			return authority.NewKit(current, e.now())
		})
	case "layout":
		return c.layout(ctx, authority.Phase(*phase), *mps, authority.LayoutOptions{KeepChain: *keepChain, DropOld: *dropOld})
	default:
		return c.inspect(ctx)
	}
}

// change writes what step makes of the secret's files, and prints the roles it wrote.
func (c secretCommand) change(ctx context.Context, step func(authority.Files) (authority.Files, error)) error {
	current, _, err := c.secrets.Read(ctx, c.name)
	if err != nil {
		return err
	}
	next, err := step(current)
	if err != nil {
		return err
	}
	if err := c.secrets.Write(ctx, c.name, next); err != nil {
		return err
	}
	return c.print(next)
}

func (c secretCommand) layout(ctx context.Context, phase authority.Phase, mps string, opts authority.LayoutOptions) error {
	switch authority.MPSChoice(mps) {
	case authority.MPSOld, authority.MPSNew:
		opts.MPS = authority.MPSChoice(mps)
	default:
		return fmt.Errorf("-mps takes %q or nothing, not %q", authority.MPSNew, mps)
	}
	if err := c.change(ctx, func(current authority.Files) (authority.Files, error) {
		return authority.Layout(current, phase, opts)
	}); err != nil {
		return err
	}
	_, err := fmt.Fprintln(c.out, "restart the server so it loads these roles")
	return err
}

func (c secretCommand) inspect(ctx context.Context) error {
	files, found, err := c.secrets.Read(ctx, c.name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("secret %s does not exist: run `make authority-init` on a new cluster", c.name)
	}
	return c.print(files)
}

// adopt moves the master key out of the secret it shared with the database passwords. The key is
// removed from there only once its own secret holds that same key.
func (c secretCommand) adopt(ctx context.Context, legacyDir, legacySecret string) error {
	if legacyDir == "" {
		return errors.New("adopt needs -legacy-dir, the shared secret's master key mounted as files")
	}
	legacy, err := readLegacy(legacyDir)
	if err != nil {
		return err
	}
	current, _, err := c.secrets.Read(ctx, c.name)
	if err != nil {
		return err
	}
	files, adopted, err := authority.Adopt(current, legacy)
	if errors.Is(err, authority.ErrNoIssuer) {
		return fmt.Errorf("%w: run `make authority-init` on a new cluster", err)
	}
	if err != nil {
		return err
	}
	if adopted {
		if err := c.secrets.Write(ctx, c.name, files); err != nil {
			return err
		}
	}
	if len(legacy[cert.IssuerKeyFile]) > 0 {
		if !bytes.Equal(legacy[cert.IssuerKeyFile], files[cert.IssuerKeyFile]) {
			return fmt.Errorf("the master key in %s differs from the one in %s; neither is changed", legacySecret, c.name)
		}
		if err := c.secrets.RemoveKeys(ctx, legacySecret, cert.IssuerCertFile, cert.IssuerKeyFile); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "the master key now lives in %s alone\n", c.name)
	}
	return c.print(files)
}

// readLegacy reads the master key from the shared secret's volume, whose items are optional.
func readLegacy(dir string) (authority.Files, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	defer root.Close()
	files := authority.Files{}
	for _, name := range []string{cert.IssuerCertFile, cert.IssuerKeyFile} {
		data, err := root.ReadFile(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		files[name] = data
	}
	return files, nil
}

func (c secretCommand) print(files authority.Files) error {
	lines, err := authority.Inspect(files)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "%s:\n", c.name)
	for _, line := range lines {
		fmt.Fprintln(c.out, "  "+line)
	}
	return nil
}

func (e env) flags(command string) *flag.FlagSet {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(e.out)
	return fs
}
