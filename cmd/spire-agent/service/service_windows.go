//go:build windows
// +build windows

package service

import (
	"fmt"
	"os"

	"github.com/spiffe/spire/cmd/spire-agent/cli"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
)

var (
	serviceName = "spiservice"
	elog        debug.Log
)

func (r Runner) Run() int {
	runService(serviceName)
	return 0
}

func (r Runner) NeedToRunAsAService() bool {
	isWindowsService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isWindowsService
}

type SpireAgentService struct{}

func (m *SpireAgentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	elog.Info(1, fmt.Sprintf("Args : %s", args))
	go func() {
		elog.Info(1, fmt.Sprintf("server agent is starting with %s", args[1:]))
		runArgs := append([]string{"run"}, args[1:]...)
		rc := new(cli.CLI).Run(runArgs)
		elog.Info(1, "server agent exited")
		os.Exit(rc)
	}()
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				elog.Info(1, "service interrogated")
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				elog.Info(1, "service stop")
				break loop
			case svc.Pause:
				elog.Info(1, "service pause")
				changes <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				elog.Info(1, "service continue")
				changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			default:
				elog.Error(1, fmt.Sprintf("unexpected control request #%d", c))
			}
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}

func runService(name string) {
	var err error
	elog, err = eventlog.Open(name)
	if err != nil {
		return
	}
	defer elog.Close()

	elog.Info(1, fmt.Sprintf("starting %s service", name))
	elog.Info(1, fmt.Sprintf("Exe args: %s", os.Args[1:]))
	run := svc.Run
	err = run(name, &SpireAgentService{})
	if err != nil {
		elog.Error(1, fmt.Sprintf("%s service failed: %v", name, err))
		return
	}
	elog.Info(1, fmt.Sprintf("%s service stopped", name))
}
