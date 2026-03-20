package process

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ProcInfo struct {
	PID      int       `json:"pid"`
	Started  time.Time `json:"started"`
	Command  string    `json:"command"`
	StartRaw string    `json:"start_raw"`
}

func List(match string) ([]ProcInfo, error) {
	cmd := exec.Command("ps", "-Ao", "pid,lstart,command")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("执行ps失败: %w", err)
	}
	return parsePS(out, match), nil
}

func IsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "operation not permitted") || strings.Contains(s, "permission denied")
}

func Kill(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

func parsePS(out []byte, match string) []ProcInfo {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 4096), 8*1024*1024)
	needMatch := strings.TrimSpace(match) != ""
	list := make([]ProcInfo, 0, 256)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.Contains(strings.ToLower(line), "pid") && strings.Contains(strings.ToLower(line), "command") {
				continue
			}
		}
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		startRaw := strings.Join(fields[1:6], " ")
		started, err := time.Parse("Mon Jan _2 15:04:05 2006", startRaw)
		if err != nil {
			continue
		}
		command := strings.Join(fields[6:], " ")
		if needMatch && !strings.Contains(command, match) {
			continue
		}
		list = append(list, ProcInfo{
			PID:      pid,
			Started:  started,
			Command:  command,
			StartRaw: startRaw,
		})
	}
	return list
}
