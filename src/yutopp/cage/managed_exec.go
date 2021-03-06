//
// Copyright yutopp 2014 - .
//
// Distributed under the Boost Software License, Version 1.0.
// (See accompanying file LICENSE_1_0.txt or copy at
// http://www.boost.org/LICENSE_1_0.txt)
//

// +build linux

package torigoya

// #include <sys/resource.h>
import "C"

import(
	"bytes"
	"strings"
	"errors"
	"fmt"
	"time"
	"os"
	"os/exec"
	"syscall"
	"runtime"
	"log"
)


//
type ResourceLimit struct {
	CPU		uint64
	AS		uint64
	FSize	uint64
}

//
var errorSequence = []byte{ 0x0d, 0x0e, 0x0a, 0x0d }


//
func (bm *BridgeMessage) managedExec(
	rl					*ResourceLimit,
	args				[]string,
	envs				map[string]string,
	umask				int,
	stdin_file_path		*string,
) (*ExecutedResult, error) {
	// make a pipe for error reports
	error_pipe, err := makePipeCloseOnExec()
	if err != nil { return nil, err }
	defer error_pipe.Close()

	log.Printf(">> managedExec start\n")

	//
	if err := bm.Pipes.Stdout.ToCloseOnExec(); err != nil {
		return nil, err
	}
	if err := bm.Pipes.Stderr.ToCloseOnExec(); err != nil {
		return nil, err
	}
	if err := bm.Pipes.Result.ToCloseOnExec(); err != nil {
		return nil, err
	}

	// fork process!
	pid, err := fork()
	if err != nil {
		return nil, err;
	}
	if pid == 0 {
		// !! call child process !!
		log.Printf(">> managedExec || child\n")

		bm.managedExecChild(rl, *error_pipe, args, envs, umask, stdin_file_path)
		return nil, nil

	} else {
		// parent process
		log.Printf(">> managedExec || parent\n")

		//
		defer func() {
			umountJail(bm.ChrootPath);
		}()

		//
		bm.Pipes.Stdout.Close()
		bm.Pipes.Stderr.Close()
		bm.Pipes.Result.CloseRead()
		error_pipe.CloseWrite()

		//
		process, err := os.FindProcess(pid)
		if err != nil {
			return nil, err;
		}

		// parent process
		wait_pid_chan := make(chan *os.ProcessState)
		go func(wait_pid_chan chan *os.ProcessState, process *os.Process) {
			ps, _ := process.Wait()
			wait_pid_chan <- ps
		}(wait_pid_chan, process)

		//
		pass_kill_chan := make(chan bool)
		go func(pass_kill_chan chan bool, rl *ResourceLimit, pid int) {
			select {
			case <-pass_kill_chan:
				/* DO NOTHING */
				log.Println("PASS")

			case <-time.After(time.Duration(rl.CPU + 5) * time.Second):
				log.Printf("Kill a sleeping process(%d).\n", pid)
				if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
					log.Printf("Failed to kill a sleeping process(%d).\n", pid)
				}
			}
		}(pass_kill_chan, rl, pid)

		//
		select {
		case ps := <-wait_pid_chan:
			close(pass_kill_chan)

			// take status
			usage, ok := ps.SysUsage().(*syscall.Rusage)
			if !ok {
				return nil, errors.New("failed to cast to *syscall.Rusage")
			}
			log.Printf("Usage %v\n", usage)

			// error check sequence
			error_buf := make([]byte, 128)
			error_len, _ := syscall.Read(error_pipe.ReadFd, error_buf)
			if error_len < len(errorSequence) {
				// execution was succeeded
				wait_status, ok := ps.Sys().(syscall.WaitStatus)
				if !ok {
					return nil, errors.New("failed to cast to syscall.WaitStatus")
				}

				// take signal
				signal := func() *syscall.Signal {
					switch {
					case wait_status.Signaled():
						s := wait_status.Signal()
						return &s
						/*
					case wait_status.Stopped():
						return wait_status.StopSignal()
*/
					default:
						return nil
					}
				}()

				// exit status
				return_code := wait_status.ExitStatus()

				// CPU time
				user_time := usage.Utime
				system_time := usage.Stime

				cpu_time := float32(user_time.Nano()) / 1e9 + float32(system_time.Nano()) / 1e9

				// Memory usage
				// usage.Maxrss -> Amount of memory usage (KB)
				// TODO: fix it
				memory := uint64(usage.Maxrss * 1024)

				// take status
				status := func() ExecutedStatus {
					if signal != nil {
						if *signal == syscall.SIGXCPU {
							return CPULimit
						}
					}
					if cpu_time > float32(rl.CPU) {
						return CPULimit
					}

					if ps.Success() {
						return Passed
					} else {
						return Error
					}
				}()

				// make result
				return &ExecutedResult{
					UsedCPUTimeSec: cpu_time,
					UsedMemoryBytes: memory,
					Signal: signal,
					ReturnCode: return_code,
					CommandLine: strings.Join(args, " "),
					Status: status,
				}, nil

			} else {
				// execution was failed
				if bytes.Equal(error_buf[:len(errorSequence)], errorSequence) {
					error_log := string(error_buf[4:error_len])
					for {
						size, err := syscall.Read(error_pipe.ReadFd, error_buf)
						if err != nil || size == 0 {
							break
						}

						error_log += string(error_buf[:size])
					}

					return nil, errors.New(error_log)

				} else {
					// invalid error byte sequence
					return nil, errors.New("invalid error byte sequence")
				}
			}

		case <-time.After(time.Duration(rl.CPU * 2 + 10) * time.Second):
			// unexpected timeout
			return nil, errors.New("Unexpected TLE...")
		}
	}
}


func (bm *BridgeMessage) managedExecChild(
	rl					*ResourceLimit,
	error_pipe			Pipe/*close on exec*/,
	args				[]string,
	envs				map[string]string,
	umask				int,
	stdin_file_path		*string,
) {
	// if called this function, child process is failed to execute
	defer func() {
		// mark failed result
		syscall.Close(error_pipe.ReadFd)
		syscall.Write(error_pipe.WriteFd, errorSequence)		// write error sequence
		if r := recover(); r != nil {
			if err, ok := r.(error); ok {
				syscall.Write(error_pipe.WriteFd, []byte(err.Error()))	// write panic sentence
			}
		}
		syscall.Close(error_pipe.WriteFd)

		// exit
		os.Exit(-1)
	}()


	// !!! ===================
	// Drop privilege
	// !! IMPORTANT !!
	// !!! ===================
	if err := bm.IntoJail(); err != nil {
		panic(err)
	}
	// !!! ===================


	log.Printf("==================================================\n")
	out, err := exec.Command("/bin/ps", "aux").Output()
	if err != nil {
		log.Printf("error:: %s\n", err.Error())
	} else {
		log.Printf("passed:: \n%s\n", out)
	}

	log.Printf("==================================================\n")
	out, err = exec.Command("/bin/ls", "-la", "/").Output()
	if err != nil {
		log.Printf("error:: %s\n", err.Error())
	} else {
		log.Printf("passed:: \n%s\n", out)
	}

	log.Printf("==================================================\n")
	out, err = exec.Command("/bin/ls", "-laR", "/home").Output()
	if err != nil {
		log.Printf("error:: %s\n", err.Error())
	} else {
		log.Printf("passed:: \n%s\n", out)
	}

	log.Printf("== Managed: child           (%v)\n", args)
	log.Printf("== Managed: envs            (%v)\n", envs)
	log.Printf("== Managed: CPU(sec)        (%v)\n", rl.CPU)
	log.Printf("== Managed: memory(byte)    (%v)\n", rl.AS)
	log.Printf("== Managed: fsize           (%v)\n", rl.FSize)

	// limit(1/2)
 	setLimit(C.RLIMIT_CORE, 0)			// Process can NOT create CORE file
 	setLimit(C.RLIMIT_NOFILE, 512)		// Process can open 512 files
	setLimit(C.RLIMIT_NPROC, 30)		// Process can create processes to 30
 	setLimit(C.RLIMIT_MEMLOCK, 1024)	// Process can lock 1024 Bytes by mlock(2)

	//
	syscall.Umask(umask)

	// set PATH env
	if path, ok := envs["PATH"]; ok {
		if err := os.Setenv("PATH", path); err != nil {
			panic(err)
		}
	}

	//
	if len(args) < 1 {
		panic(errors.New("args must contain at least one element"))
	}
	command := args[0]	// args[0] is program name
	exec_path, err := exec.LookPath(command)
	if err != nil {
		panic(err)
	}

	//
	var env_list []string
	for k, v := range envs {
		env_list = append(env_list, k + "=" + v)
	}

	log.Printf("managed exec :: syscall.Exec!\n")

	// close unused pipe
	if err := bm.Pipes.Result.Close(); err != nil { panic(err) }

	// redirect stdin
	if stdin_file_path != nil {
		log.Printf("============= stdin (%v)\n", *stdin_file_path)
		file, err := os.Open(*stdin_file_path)	// read
		if err != nil { panic(err) }
		defer file.Close()

		//
		if err := syscall.Dup2(int(file.Fd()), 0); err != nil { panic(err) }
	}

	// redirect stdout
	if err := bm.Pipes.Stdout.CloseRead(); err != nil { panic(err) }
	if err := syscall.Dup2(bm.Pipes.Stdout.WriteFd, 1); err != nil { panic(err) }
	if err := bm.Pipes.Stdout.CloseWrite(); err != nil { panic(err) }

	// redirect stderr
	if err := bm.Pipes.Stderr.CloseRead(); err != nil { panic(err) }
	if err := syscall.Dup2(bm.Pipes.Stderr.WriteFd, 2); err != nil { panic(err) }
	if err := bm.Pipes.Stderr.CloseWrite(); err != nil { panic(err) }

	// limit(2/2)
	setLimitWithMarginSec(C.RLIMIT_CPU, rl.CPU)		// CPU can be used only cpu_limit_time(sec)
 	setLimit(C.RLIMIT_AS, rl.AS)					// Memory can be used only memory_limit_bytes [be careful!]
 	setLimit(C.RLIMIT_FSIZE, rl.FSize)				// Process can writes a file only FSize Bytes

	// ==========
	// exec!!
	err = syscall.Exec(exec_path, args, env_list);

	panic(errors.New(fmt.Sprintf("UNREACHABLE!! managedExecChild / failed to Exec. Error => " + err.Error())))
}


func fork() (int, error) {
	runtime.LockOSThread()
	syscall.ForkLock.Lock()
	pid, _, err := syscall.Syscall(syscall.SYS_FORK, 0, 0, 0)
	syscall.ForkLock.Unlock()
	runtime.UnlockOSThread()

	if err != 0 {
		return -1, err
	}
	return int(pid), nil
}


func setLimit(resource int, value uint64) {
	if err := syscall.Setrlimit(resource, &syscall.Rlimit{value, value}); err != nil {
		panic(err)
	}
}

func setLimitSoftHard(resource int, solf_value uint64, hard_value uint64) {
	if err := syscall.Setrlimit(resource, &syscall.Rlimit{solf_value, hard_value}); err != nil {
		panic(err)
	}
}

func setLimitWithMarginSec(resource int, value uint64) {
	setLimitSoftHard(resource, value + 1, value + 2)
}
