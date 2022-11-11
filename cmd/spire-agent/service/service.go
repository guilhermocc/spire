package service

type runAsASystemService func(args []string) bool

type Runner struct {
	args []string
}

func NewRunner(args []string) *Runner {
	return &Runner{args: args}
}
