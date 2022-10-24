package cliprinter

import (
	"errors"
	"flag"
	"fmt"

	commoncli "github.com/spiffe/spire/pkg/common/cli"
)

// AppendFlag adds the -format flag to the provided flagset, and populates
// the referenced Printer interface with a properly configured printer.
func AppendFlag(p *Printer, fs *flag.FlagSet, env *commoncli.Env) {
	AppendFlagWithCustomPretty(p, fs, env, nil)
}

// AppendFlagWithCustomPretty is the same as AppendFlag, however it also allows
// a custom pretty function to be specified. A custom pretty function can be used
// to override the pretty print logic that normally ships with this package. Its
// intended use is to allow for the adoption of cliprinter while still retaining
// backwards compatibility with the legacy/bespoke pretty print output.
func AppendFlagWithCustomPretty(p *Printer, fs *flag.FlagSet, env *commoncli.Env, cp CustomPrettyFunc) {
	// Set the default
	np := newPrinter(defaultFormatType, env)
	np.setCustomPrettyPrinter(cp)
	*p = np

	f := &FormatterFlag{
		p:            p,
		f:            defaultFormatType,
		env:          env,
		customPretty: cp,
	}

	fs.Var(f, "format", "Desired output format (pretty, json)")
}

type FormatterFlag struct {
	customPretty CustomPrettyFunc

	// A pointer to our consumer's Printer interface, along with
	// its format type
	p   *Printer
	env *commoncli.Env
	f   formatType
}

func (f *FormatterFlag) String() string {
	if f == nil || f.f == 0 {
		return formatTypeToStr(defaultFormatType)
	}

	return formatTypeToStr(f.f)
}

func (f *FormatterFlag) Set(formatStr string) error {
	if f.p == nil {
		return errors.New("internal error: formatter flag not correctly invoked; please report this bug")
	}

	format, err := strToFormatType(formatStr)
	if err != nil {
		return fmt.Errorf("bad formatter flag: %w", err)
	}

	np := newPrinter(format, f.env)
	np.setCustomPrettyPrinter(f.customPretty)

	*f.p = np
	f.f = format
	return nil
}
