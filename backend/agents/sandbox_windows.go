//go:build windows

package agents

import (
	"os/exec"
	"reflect"
	"syscall"
	"unsafe"
)

// Job Object с JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE: при закрытии последнего
// хэндла job ОС гарантированно убивает ВСЕ процессы дерева (runner и все
// запущенные им aider), включая тех, кто пережил родителя.

type jobObject uintptr

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
)

const (
	jobObjectExtendedLimitInformationClass = 9
	jobObjectLimitKillOnJobClose           = 0x00002000
)

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobExtendedLimitInformation struct {
	BasicLimitInformation   jobBasicLimitInformation
	IoInfo                  ioCounters
	ProcessMemoryLimit      uintptr
	JobMemoryLimit          uintptr
	PeakProcessMemoryUsed   uintptr
	PeakJobMemoryUsed       uintptr
}

// processHandle достаёт unexported-хэндл из os.Process (публичного доступа нет).
func processHandle(cmd *exec.Cmd) uintptr {
	if cmd.Process == nil {
		return 0
	}
	v := reflect.ValueOf(cmd.Process).Elem().FieldByName("handle")
	if !v.IsValid() {
		return 0
	}
	return uintptr(v.Uint())
}

// attachJobObject создаёт job с kill-on-close и назначает ему процесс.
// Возвращает 0, если создать/назначить job не удалось (fallback на Process.Kill).
func attachJobObject(cmd *exec.Cmd) jobObject {
	h, _, _ := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return 0
	}
	info := jobExtendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r1, _, _ := procSetInformationJobObject.Call(
		h,
		jobObjectExtendedLimitInformationClass,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r1 == 0 {
		_, _, _ = procCloseHandle.Call(h)
		return 0
	}
	if r1, _, _ = procAssignProcessToJobObject.Call(h, processHandle(cmd)); r1 == 0 {
		_, _, _ = procCloseHandle.Call(h)
		return 0
	}
	return jobObject(h)
}

func (j jobObject) close() { _, _, _ = procCloseHandle.Call(uintptr(j)) }

func applyProcAttrs(*exec.Cmd) {}

// killTree закрывает job — ОС убивает все процессы дерева.
func (p *proc) killTree() {
	p.releaseJob()
	if p.job == 0 {
		_ = p.cmd.Process.Kill()
	}
}

// releaseJob идемпотентно закрывает хэндл job (после Wait — чтобы не течь).
func (p *proc) releaseJob() {
	p.jobMu.Lock()
	defer p.jobMu.Unlock()
	if p.job != 0 && !p.jobDone {
		p.job.close()
		p.jobDone = true
	}
}
