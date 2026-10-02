// Package envfile loads a dotenv file into the process environment, shared by
// every command so they handle configuration files the same way.
package envfile

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"

	"github.com/joho/godotenv"
)

// DefaultPath is loaded when -env-file is not given.
const DefaultPath = ".env"

const flagName = "env-file"

// Load reads path into the environment. Variables already set in the real
// environment win over the file. A missing file is an error only when
// required: the default .env is optional (e.g. in containers), but a path the
// user asked for must exist.
func Load(path string, required bool) error {
	err := godotenv.Load(path)
	if err == nil || (!required && errors.Is(err, fs.ErrNotExist)) {
		return nil
	}
	return fmt.Errorf("load env file %s: %w", path, err)
}

// Register adds the -env-file flag to flags and returns a function that loads
// the selected file; call it after flags.Parse.
func Register(flags *flag.FlagSet) (load func() error) {
	path := flags.String(flagName, DefaultPath, "dotenv file to load (real environment variables take precedence)")
	return func() error {
		explicit := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == flagName {
				explicit = true
			}
		})
		return Load(*path, explicit)
	}
}
