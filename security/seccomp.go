package security

import (
	"fmt"

	seccomp "github.com/seccomp/libseccomp-golang"
)

// ApplySeccomp applies a seccomp profile to the current process.
func ApplySeccomp(seccompConfig Seccomp) error {
	// Parse default action
	parseValue := parseAction(seccompConfig.DefaultAction, seccompConfig.DefaultErrnoRet)

	// Create seccomp filter
	filter, err := seccomp.NewFilter(parseValue)
	if err != nil {
		return fmt.Errorf("failed to create seccomp filter: %v", err)
	}
	defer filter.Release()

	// architure config
	for _, archEntry := range seccompConfig.ArchMap {
		mainArch, err := seccomp.GetArchFromString(stripPrefix(archEntry.Architecture))
		if err == nil {
			_ = filter.AddArch(mainArch)
		}
		for _, sub := range archEntry.SubArchitectures {
			subArch, err := seccomp.GetArchFromString(stripPrefix(sub))
			if err == nil {
				_ = filter.AddArch(subArch)
			}
		}
	}

	// Process syscall rules
	for _, rule := range seccompConfig.Syscalls {
		action := parseAction(rule.Action, seccompConfig.DefaultErrnoRet)
		if rule.ErrnoRet != nil {
			action = seccomp.ActErrno.SetReturnCode(int16(*rule.ErrnoRet))
		}
		for _, name := range rule.Names {
			sc, err := seccomp.GetSyscallFromName(name)
			if err != nil {
				continue
			}
			if len(rule.Args) == 0 {
				_ = filter.AddRule(sc, action)
			} else {
				var conditions []seccomp.ScmpCondition
				for _, arg := range rule.Args {
					conditions = append(conditions, seccomp.ScmpCondition{
						Argument: arg.Index,
						Op:       parseOperator(arg.Op),
						Operand1: arg.Value,
						Operand2: 0,
					})
				}
				_ = filter.AddRuleConditional(sc, action, conditions)
			}
		}
	}

	// Load the filter
	if err := filter.Load(); err != nil {
		return fmt.Errorf("failed to load seccomp filter: %v", err)
	}

	// fmt.Printf("Applied seccomp profile with %d syscall rules\n", len(seccompConfig.Syscalls))
	return nil
}
