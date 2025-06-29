package security

import (
	"strings"

	seccomp "github.com/seccomp/libseccomp-golang"
	"github.com/syndtr/gocapability/capability"
)

type ArchMapEntry struct {
	Architecture     string   `json:"architecture"`
	SubArchitectures []string `json:"subArchitectures"`
}

type Capabilities struct {
	Bounding    []string `json:"bounding"`
	Effective   []string `json:"effective"`
	Inheritable []string `json:"inheritable"`
	Permitted   []string `json:"permitted"`
	Ambient     []string `json:"ambient"`
}

type Seccomp struct {
	DefaultAction   string         `json:"defaultAction"`
	DefaultErrnoRet uint           `json:"defaultErrnoRet"`
	ArchMap         []ArchMapEntry `json:"archMap"`
	Syscalls        []SyscallRule  `json:"syscalls"`
}

type SyscallRule struct {
	Names    []string     `json:"names"`
	Action   string       `json:"action"`
	ErrnoRet *uint        `json:"errnoRet,omitempty"`
	Args     []SyscallArg `json:"args,omitempty"`
}

type SyscallArg struct {
	Index uint   `json:"index"`
	Value uint64 `json:"value"`
	Op    string `json:"op"`
}

type Rlimit struct {
	Type string `json:"type"`
	Hard uint64 `json:"hard"`
	Soft uint64 `json:"soft"`
}

type Config struct {
	Capabilities Capabilities `json:"capabilities"`
	Rlimit       []Rlimit     `json:"rlimits"`
	Seccomp      Seccomp      `json:"seccomp"`
	RootfsPath   string       `json:"rootfs"`
	MergedPath   string       `json:"merged"`
	UpperPath    string       `json:"upper"`
	WorkPath     string       `json:"work"`
	// Add other fields hered
}

// Capability name to number mapping
var capabilityMap = map[string]capability.Cap{
	"CAP_CHOWN":            capability.CAP_CHOWN,
	"CAP_DAC_OVERRIDE":     capability.CAP_DAC_OVERRIDE,
	"CAP_DAC_READ_SEARCH":  capability.CAP_DAC_READ_SEARCH,
	"CAP_FOWNER":           capability.CAP_FOWNER,
	"CAP_FSETID":           capability.CAP_FSETID,
	"CAP_KILL":             capability.CAP_KILL,
	"CAP_SETGID":           capability.CAP_SETGID,
	"CAP_SETUID":           capability.CAP_SETUID,
	"CAP_SETPCAP":          capability.CAP_SETPCAP,
	"CAP_LINUX_IMMUTABLE":  capability.CAP_LINUX_IMMUTABLE,
	"CAP_NET_BIND_SERVICE": capability.CAP_NET_BIND_SERVICE,
	"CAP_NET_BROADCAST":    capability.CAP_NET_BROADCAST,
	"CAP_NET_ADMIN":        capability.CAP_NET_ADMIN,
	"CAP_NET_RAW":          capability.CAP_NET_RAW,
	"CAP_IPC_LOCK":         capability.CAP_IPC_LOCK,
	"CAP_IPC_OWNER":        capability.CAP_IPC_OWNER,
	"CAP_SYS_MODULE":       capability.CAP_SYS_MODULE,
	"CAP_SYS_RAWIO":        capability.CAP_SYS_RAWIO,
	"CAP_SYS_CHROOT":       capability.CAP_SYS_CHROOT,
	"CAP_SYS_PTRACE":       capability.CAP_SYS_PTRACE,
	"CAP_SYS_PACCT":        capability.CAP_SYS_PACCT,
	"CAP_SYS_ADMIN":        capability.CAP_SYS_ADMIN,
	"CAP_SYS_BOOT":         capability.CAP_SYS_BOOT,
	"CAP_SYS_NICE":         capability.CAP_SYS_NICE,
	"CAP_SYS_RESOURCE":     capability.CAP_SYS_RESOURCE,
	"CAP_SYS_TIME":         capability.CAP_SYS_TIME,
	"CAP_SYS_TTY_CONFIG":   capability.CAP_SYS_TTY_CONFIG,
	"CAP_MKNOD":            capability.CAP_MKNOD,
	"CAP_LEASE":            capability.CAP_LEASE,
	"CAP_AUDIT_WRITE":      capability.CAP_AUDIT_WRITE,
	"CAP_AUDIT_CONTROL":    capability.CAP_AUDIT_CONTROL,
	"CAP_SETFCAP":          capability.CAP_SETFCAP,
	"CAP_MAC_OVERRIDE":     capability.CAP_MAC_OVERRIDE,
	"CAP_MAC_ADMIN":        capability.CAP_MAC_ADMIN,
	"CAP_SYSLOG":           capability.CAP_SYSLOG,
	"CAP_WAKE_ALARM":       capability.CAP_WAKE_ALARM,
	"CAP_BLOCK_SUSPEND":    capability.CAP_BLOCK_SUSPEND,
	"CAP_AUDIT_READ":       capability.CAP_AUDIT_READ,
}

func stripPrefix(s string) string {
	// Turns "SCMP_ARCH_X86_64" → "x86_64"
	return strings.ToLower(strings.TrimPrefix(s, "SCMP_ARCH_"))
}

func parseAction(action string, errno uint) seccomp.ScmpAction {
	// fmt.Printf("errno for parseaction func: %v", errno)
	switch action {
	case "SCMP_ACT_ALLOW":
		return seccomp.ActAllow
	case "SCMP_ACT_ERRNO":
		return seccomp.ActErrno.SetReturnCode(int16(errno))
	case "SCMP_ACT_KILL":
		return seccomp.ActKill
	default:
		return seccomp.ActAllow
	}
}

func parseOperator(op string) seccomp.ScmpCompareOp {
	// fmt.Println(op)
	switch op {
	case "SCMP_CMP_EQ":
		return seccomp.CompareEqual
	case "SCMP_CMP_NE":
		return seccomp.CompareNotEqual
	case "SCMP_CMP_LT":
		return seccomp.CompareLess
	case "SCMP_CMP_LE":
		return seccomp.CompareLessOrEqual
	case "SCMP_CMP_MASKED_EQ":
		return seccomp.CompareMaskedEqual
	default:
		panic("unsupported operator: " + op)
	}
}
