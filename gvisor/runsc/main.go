// Copyright 2018 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Binary runsc is an implementation of the Open Container Initiative Runtime
// that runs applications inside a sandbox.
package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"strconv"
	"math"
	"bytes"
	"encoding/binary"

	"github.com/google/subcommands"
	"gvisor.dev/gvisor/pkg/log"
	"gvisor.dev/gvisor/pkg/refs"
	"gvisor.dev/gvisor/pkg/sentry/platform"
	"gvisor.dev/gvisor/runsc/boot"
	"gvisor.dev/gvisor/runsc/cmd"
	"gvisor.dev/gvisor/runsc/flag"
	"gvisor.dev/gvisor/runsc/specutils"

	"os/exec"
	"encoding/json"
	"gvisor.dev/gvisor/pkg/maid"
)

var (
	// Although these flags are not part of the OCI spec, they are used by
	// Docker, and thus should not be changed.
	rootDir     = flag.String("root", "", "root directory for storage of container state.")
	logFilename = flag.String("log", "", "file path where internal debug information is written, default is stdout.")
	logFormat   = flag.String("log-format", "text", "log format: text (default), json, or json-k8s.")
	debug       = flag.Bool("debug", false, "enable debug logging.")
	showVersion = flag.Bool("version", false, "show version and exit.")
	// TODO(gvisor.dev/issue/193): support systemd cgroups
	systemdCgroup = flag.Bool("systemd-cgroup", false, "Use systemd for cgroups. NOT SUPPORTED.")

	// These flags are unique to runsc, and are used to configure parts of the
	// system that are not covered by the runtime spec.

	// Debugging flags.
	debugLog        = flag.String("debug-log", "", "additional location for logs. If it ends with '/', log files are created inside the directory with default names. The following variables are available: %TIMESTAMP%, %COMMAND%.")
	panicLog        = flag.String("panic-log", "", "file path were panic reports and other Go's runtime messages are written.")
	logPackets      = flag.Bool("log-packets", false, "enable network packet logging.")
	logFD           = flag.Int("log-fd", -1, "file descriptor to log to.  If set, the 'log' flag is ignored.")
	debugLogFD      = flag.Int("debug-log-fd", -1, "file descriptor to write debug logs to.  If set, the 'debug-log-dir' flag is ignored.")
	panicLogFD      = flag.Int("panic-log-fd", -1, "file descriptor to write Go's runtime messages.")
	debugLogFormat  = flag.String("debug-log-format", "text", "log format: text (default), json, or json-k8s.")
	alsoLogToStderr = flag.Bool("alsologtostderr", false, "send log messages to stderr.")

	// Debugging flags: strace related
	strace         = flag.Bool("strace", false, "enable strace.")
	straceSyscalls = flag.String("strace-syscalls", "", "comma-separated list of syscalls to trace. If --strace is true and this list is empty, then all syscalls will be traced.")
	straceLogSize  = flag.Uint("strace-log-size", 1024, "default size (in bytes) to log data argument blobs.")

	// Flags that control sandbox runtime behavior.
	platformName       = flag.String("platform", "ptrace", "specifies which platform to use: ptrace (default), kvm.")
	network            = flag.String("network", "sandbox", "specifies which network to use: sandbox (default), host, none. Using network inside the sandbox is more secure because it's isolated from the host network.")
	hardwareGSO        = flag.Bool("gso", true, "enable hardware segmentation offload if it is supported by a network device.")
	softwareGSO        = flag.Bool("software-gso", true, "enable software segmentation offload when hardware offload can't be enabled.")
	txChecksumOffload  = flag.Bool("tx-checksum-offload", false, "enable TX checksum offload.")
	rxChecksumOffload  = flag.Bool("rx-checksum-offload", true, "enable RX checksum offload.")
	qDisc              = flag.String("qdisc", "fifo", "specifies which queueing discipline to apply by default to the non loopback nics used by the sandbox.")
	fileAccess         = flag.String("file-access", "exclusive", "specifies which filesystem to use for the root mount: exclusive (default), shared. Volume mounts are always shared.")
	fsGoferHostUDS     = flag.Bool("fsgofer-host-uds", false, "allow the gofer to mount Unix Domain Sockets.")
	overlay            = flag.Bool("overlay", false, "wrap filesystem mounts with writable overlay. All modifications are stored in memory inside the sandbox.")
	overlayfsStaleRead = flag.Bool("overlayfs-stale-read", true, "assume root mount is an overlay filesystem")
	watchdogAction     = flag.String("watchdog-action", "log", "sets what action the watchdog takes when triggered: log (default), panic.")
	panicSignal        = flag.Int("panic-signal", -1, "register signal handling that panics. Usually set to SIGUSR2(12) to troubleshoot hangs. -1 disables it.")
	profile            = flag.Bool("profile", false, "prepares the sandbox to use Golang profiler. Note that enabling profiler loosens the seccomp protection added to the sandbox (DO NOT USE IN PRODUCTION).")
	netRaw             = flag.Bool("net-raw", false, "enable raw sockets. When false, raw sockets are disabled by removing CAP_NET_RAW from containers (`runsc exec` will still be able to utilize raw sockets). Raw sockets allow malicious containers to craft packets and potentially attack the network.")
	numNetworkChannels = flag.Int("num-network-channels", 1, "number of underlying channels(FDs) to use for network link endpoints.")
	rootless           = flag.Bool("rootless", false, "it allows the sandbox to be started with a user that is not root. Sandbox and Gofer processes may run with same privileges as current user.")
	referenceLeakMode  = flag.String("ref-leak-mode", "disabled", "sets reference leak check mode: disabled (default), log-names, log-traces.")
	cpuNumFromQuota    = flag.Bool("cpu-num-from-quota", false, "set cpu number to cpu quota (least integer greater or equal to quota value, but not less than 2)")
	vfs2Enabled        = flag.Bool("vfs2", false, "TEST ONLY; use while VFSv2 is landing. This uses the new experimental VFS layer.")
	fuseEnabled        = flag.Bool("fuse", false, "TEST ONLY; use while FUSE in VFSv2 is landing. This allows the use of the new experimental FUSE filesystem.")

	// Test flags, not to be used outside tests, ever.
	testOnlyAllowRunAsCurrentUserWithoutChroot = flag.Bool("TESTONLY-unsafe-nonroot", false, "TEST ONLY; do not ever use! This skips many security measures that isolate the host from the sandbox.")
	testOnlyTestNameEnv                        = flag.String("TESTONLY-test-name-env", "", "TEST ONLY; do not ever use! Used for automated tests to improve logging.")

	addrSendFD			= flag.Int("addr-fd", -1, "send addr and access number to sandbox.")
)

func main() {
	// Help and flags commands are generated automatically.
	help := cmd.NewHelp(subcommands.DefaultCommander)
	help.Register(new(cmd.Syscalls))
	subcommands.Register(help, "")
	subcommands.Register(subcommands.FlagsCommand(), "")

	// Installation helpers.
	const helperGroup = "helpers"
	subcommands.Register(new(cmd.Install), helperGroup)
	subcommands.Register(new(cmd.Uninstall), helperGroup)

	// Register user-facing runsc commands.
	subcommands.Register(new(cmd.Checkpoint), "")
	subcommands.Register(new(cmd.Create), "")
	subcommands.Register(new(cmd.Delete), "")
	subcommands.Register(new(cmd.Do), "")
	subcommands.Register(new(cmd.Events), "")
	subcommands.Register(new(cmd.Exec), "")
	subcommands.Register(new(cmd.Gofer), "")
	subcommands.Register(new(cmd.Kill), "")
	subcommands.Register(new(cmd.List), "")
	subcommands.Register(new(cmd.Pause), "")
	subcommands.Register(new(cmd.PS), "")
	subcommands.Register(new(cmd.Restore), "")
	subcommands.Register(new(cmd.Resume), "")
	subcommands.Register(new(cmd.Run), "")
	subcommands.Register(new(cmd.Spec), "")
	subcommands.Register(new(cmd.State), "")
	subcommands.Register(new(cmd.Start), "")
	subcommands.Register(new(cmd.Wait), "")

	// Register internal commands with the internal group name. This causes
	// them to be sorted below the user-facing commands with empty group.
	// The string below will be printed above the commands.
	const internalGroup = "internal use only"
	subcommands.Register(new(cmd.Boot), internalGroup)
	subcommands.Register(new(cmd.Debug), internalGroup)
	subcommands.Register(new(cmd.Gofer), internalGroup)
	subcommands.Register(new(cmd.Statefile), internalGroup)

	// All subcommands must be registered before flag parsing.
	flag.Parse()

	// Are we showing the version?
	if *showVersion {
		// The format here is the same as runc.
		fmt.Fprintf(os.Stdout, "runsc version %s\n", version)
		fmt.Fprintf(os.Stdout, "spec: %s\n", specutils.Version)
		os.Exit(0)
	}

	// TODO(gvisor.dev/issue/193): support systemd cgroups
	if *systemdCgroup {
		fmt.Fprintln(os.Stderr, "systemd cgroup flag passed, but systemd cgroups not supported. See gvisor.dev/issue/193")
		os.Exit(1)
	}

	var errorLogger io.Writer
	if *logFD > -1 {
		errorLogger = os.NewFile(uintptr(*logFD), "error log file")

	} else if *logFilename != "" {
		// We must set O_APPEND and not O_TRUNC because Docker passes
		// the same log file for all commands (and also parses these
		// log files), so we can't destroy them on each command.
		var err error
		errorLogger, err = os.OpenFile(*logFilename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			cmd.Fatalf("error opening log file %q: %v", *logFilename, err)
		}
	}
	cmd.ErrorLogger = errorLogger

	platformType := *platformName
	if _, err := platform.Lookup(platformType); err != nil {
		cmd.Fatalf("%v", err)
	}

	fsAccess, err := boot.MakeFileAccessType(*fileAccess)
	if err != nil {
		cmd.Fatalf("%v", err)
	}

	if fsAccess == boot.FileAccessShared && *overlay {
		cmd.Fatalf("overlay flag is incompatible with shared file access")
	}

	netType, err := boot.MakeNetworkType(*network)
	if err != nil {
		cmd.Fatalf("%v", err)
	}

	wa, err := boot.MakeWatchdogAction(*watchdogAction)
	if err != nil {
		cmd.Fatalf("%v", err)
	}

	if *numNetworkChannels <= 0 {
		cmd.Fatalf("num_network_channels must be > 0, got: %d", *numNetworkChannels)
	}

	refsLeakMode, err := boot.MakeRefsLeakMode(*referenceLeakMode)
	if err != nil {
		cmd.Fatalf("%v", err)
	}

	queueingDiscipline, err := boot.MakeQueueingDiscipline(*qDisc)
	if err != nil {
		cmd.Fatalf("%s", err)
	}

	// Sets the reference leak check mode. Also set it in config below to
	// propagate it to child processes.
	refs.SetLeakMode(refsLeakMode)

	// Create a new Config from the flags.
	conf := &boot.Config{
		RootDir:            *rootDir,
		Debug:              *debug,
		LogFilename:        *logFilename,
		LogFormat:          *logFormat,
		DebugLog:           *debugLog,
		PanicLog:           *panicLog,
		DebugLogFormat:     *debugLogFormat,
		FileAccess:         fsAccess,
		FSGoferHostUDS:     *fsGoferHostUDS,
		Overlay:            *overlay,
		Network:            netType,
		HardwareGSO:        *hardwareGSO,
		SoftwareGSO:        *softwareGSO,
		TXChecksumOffload:  *txChecksumOffload,
		RXChecksumOffload:  *rxChecksumOffload,
		LogPackets:         *logPackets,
		Platform:           platformType,
		Strace:             *strace,
		StraceLogSize:      *straceLogSize,
		WatchdogAction:     wa,
		PanicSignal:        *panicSignal,
		ProfileEnable:      *profile,
		EnableRaw:          *netRaw,
		NumNetworkChannels: *numNetworkChannels,
		Rootless:           *rootless,
		AlsoLogToStderr:    *alsoLogToStderr,
		ReferenceLeakMode:  refsLeakMode,
		OverlayfsStaleRead: *overlayfsStaleRead,
		CPUNumFromQuota:    *cpuNumFromQuota,
		VFS2:               *vfs2Enabled,
		FUSE:               *fuseEnabled,
		QDisc:              queueingDiscipline,
		TestOnlyAllowRunAsCurrentUserWithoutChroot: *testOnlyAllowRunAsCurrentUserWithoutChroot,
		TestOnlyTestNameEnv:                        *testOnlyTestNameEnv,
	}
	if len(*straceSyscalls) != 0 {
		conf.StraceSyscalls = strings.Split(*straceSyscalls, ",")
	}

	// Set up logging.
	if *debug {
		log.SetLevel(log.Debug)
	}

	// Logging will include the local date and time via the time package.
	//
	// On first use, time.Local initializes the local time zone, which
	// involves opening tzdata files on the host. Since this requires
	// opening host files, it must be done before syscall filter
	// installation.
	//
	// Generally there will be a log message before filter installation
	// that will force initialization, but force initialization here in
	// case that does not occur.
	_ = time.Local.String()

	subcommand := flag.CommandLine.Arg(0)

	var e log.Emitter
	if *debugLogFD > -1 {
		f := os.NewFile(uintptr(*debugLogFD), "debug log file")

		e = newEmitter(*debugLogFormat, f)

	} else if *debugLog != "" {
		f, err := specutils.DebugLogFile(*debugLog, subcommand, "" /* name */)
		if err != nil {
			cmd.Fatalf("error opening debug log file in %q: %v", *debugLog, err)
		}
		e = newEmitter(*debugLogFormat, f)

	} else {
		// Stderr is reserved for the application, just discard the logs if no debug
		// log is specified.
		e = newEmitter("text", ioutil.Discard)
	}

	if *panicLogFD > -1 || *debugLogFD > -1 {
		fd := *panicLogFD
		if fd < 0 {
			fd = *debugLogFD
		}
		// Quick sanity check to make sure no other commands get passed
		// a log fd (they should use log dir instead).
		if subcommand != "boot" && subcommand != "gofer" && subcommand != "monitor"{
			cmd.Fatalf("flags --debug-log-fd and --panic-log-fd should only be passed to 'boot' and 'gofer' command, but was passed to %q", subcommand)
		}

		// If we are the boot process, then we own our stdio FDs and can do what we
		// want with them. Since Docker and Containerd both eat boot's stderr, we
		// dup our stderr to the provided log FD so that panics will appear in the
		// logs, rather than just disappear.
		if err := syscall.Dup3(fd, int(os.Stderr.Fd()), 0); err != nil {
			cmd.Fatalf("error dup'ing fd %d to stderr: %v", fd, err)
		}
	} else if *alsoLogToStderr {
		e = &log.MultiEmitter{e, newEmitter(*debugLogFormat, os.Stderr)}
	}

	log.SetTarget(e)

	// =========Cijitter: strat a thread to read addr=========
	if subcommand == "boot" {
		// init listener thread
		go listener()
	}

	if subcommand == "monitor" {
		log.Debugf("[Cijitter] Start to monitor addr...")
		
		// init notifier thread
		addrChan := make(chan string, 1)
		go notifier(addrChan)

		//strat the monitor
		_, cid := filepath.Split(os.Args[35])	// get container id
		monitor(cid, addrChan)
	}
	/*===========================================*/

	log.Infof("***************************")
	log.Infof("Args: %s", os.Args)
	log.Infof("Version %s", version)
	log.Infof("PID: %d", os.Getpid())
	log.Infof("UID: %d, GID: %d", os.Getuid(), os.Getgid())
	log.Infof("Configuration:")
	log.Infof("\t\tRootDir: %s", conf.RootDir)
	log.Infof("\t\tPlatform: %v", conf.Platform)
	log.Infof("\t\tFileAccess: %v, overlay: %t", conf.FileAccess, conf.Overlay)
	log.Infof("\t\tNetwork: %v, logging: %t", conf.Network, conf.LogPackets)
	log.Infof("\t\tStrace: %t, max size: %d, syscalls: %s", conf.Strace, conf.StraceLogSize, conf.StraceSyscalls)
	log.Infof("\t\tVFS2 enabled: %v", conf.VFS2)
	log.Infof("***************************")

	if *testOnlyAllowRunAsCurrentUserWithoutChroot {
		// SIGTERM is sent to all processes if a test exceeds its
		// timeout and this case is handled by syscall_test_runner.
		log.Warningf("Block the TERM signal. This is only safe in tests!")
		signal.Ignore(syscall.SIGTERM)
	}

	// Call the subcommand and pass in the configuration.
	var ws syscall.WaitStatus
	subcmdCode := subcommands.Execute(context.Background(), conf, &ws)
	if subcmdCode == subcommands.ExitSuccess {
		log.Infof("Exiting with status: %v", ws)
		if ws.Signaled() {
			// No good way to return it, emulate what the shell does. Maybe raise
			// signal to self?
			os.Exit(128 + int(ws.Signal()))
		}
		os.Exit(ws.ExitStatus())
	}
	// Return an error that is unlikely to be used by the application.
	log.Warningf("Failure to execute command, err: %v", subcmdCode)
	os.Exit(128)
}

func newEmitter(format string, logFile io.Writer) log.Emitter {
	switch format {
	case "text":
		return log.GoogleEmitter{&log.Writer{Next: logFile}}
	case "json":
		return log.JSONEmitter{&log.Writer{Next: logFile}}
	case "json-k8s":
		return log.K8sJSONEmitter{&log.Writer{Next: logFile}}
	}
	cmd.Fatalf("invalid log format %q, must be 'text', 'json', or 'json-k8s'", format)
	panic("unreachable")
}

func init() {
	// Set default root dir to something (hopefully) user-writeable.
	*rootDir = "/var/run/runsc"
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		*rootDir = filepath.Join(runtimeDir, "runsc")
	}
}

//========================================================//
func listener() {
	reader := os.NewFile(uintptr(13), "reader")
	defer reader.Close()

	for {
		var data interface{}
		decoder := json.NewDecoder(reader)
		if err := decoder.Decode(&data); err == nil {
			log.Debugf("[Cijitter] Addr received from child pipe: %v\n", data)
			addrInfo := fmt.Sprintf("%v", data)
			maid.Listen_target_addrs(addrInfo)
		}
	}
	log.Debugf("[Cijitter] Addr listener finished!")
}

func notifier(msgChan chan string) {
	writer := os.NewFile(uintptr(11), "writer")
	defer writer.Close()

	for{
		msg := <-msgChan
		err := json.NewEncoder(writer).Encode(msg)
		if err != nil {
			log.Debugf("[Cijitter] Addr sended failed: %v", err)
		}
	}
	log.Debugf("[Cijitter] Addr notifier finished!")
}

var duration int = 8050
var interval int = 500
func monitor(cid string, msgChan chan string) {
	log.Debugf("[Cijitter] Monitor start...")

	// judge if it needs to delay
	var last_addr_acc = [3]int{500, 500, 500}
	var last_delay = [3]bool{true, true, true}
	index := 0

	// delay duration
	delay_duration := time.Duration(duration)		//6750-300, 9000-400
	delay_interval := time.Duration(interval)

	time.Sleep(40 * time.Second)

	for {
		// call kernel module
		addr, acc_num, err := get_target_addr()
		if !err {
			log.Debugf("[Cijitter] failed to get target address...")
			time.Sleep(delay_interval * time.Millisecond)
			continue
		}

		log.Debugf("[Cijitter] addr: %s, access: %d", addr, acc_num)
		addr_acc := addr + " " + strconv.Itoa(acc_num)

		inx := index % 3
		//decide the duration of delaying
		delay_int, dstats := delayStates(last_delay, index, delay_interval)
		delay_interval = delay_int
		index++

		//make up
		old_acc := last_addr_acc[inx]
		last_acc := last_addr_acc[(inx+2)%3]
		acc_cmp := 0
                if dstats && (acc_num < last_acc) {
			acc_cmp = acc_num + int(float64(last_acc - acc_num) * 0.67)
		} else {
			acc_cmp = acc_num
		}
                last_addr_acc[inx] = acc_cmp

		if acc_num > 3000 {
			last_addr_acc[inx] = old_acc
		} else if acc_cmp <= 80 || !judge_delay(last_addr_acc, inx) {
			log.Debugf("[Cijitter] this is a strip, pass... %d\n", acc_num)
			// delay in last time
			if dstats {
				last_addr_acc[inx] = old_acc
			}
			// log delay status
			last_delay[inx] = false
			time.Sleep(delay_interval * time.Millisecond)
			continue
		}

		// notify: delay target address
		if strings.Contains(addr, "0x"){
			log.Debugf("[Cijitter] start to send addr %s", cid)
			msgChan <- addr_acc
		}

		// delay time window
		time.Sleep(delay_duration * time.Millisecond)

		// notify: stop delay target address
		log.Debugf("[Cijitter] stop delay and start to profiling %s", cid)
		stopSig := "0x00000 0"
		msgChan <- stopSig
		last_delay[inx] = true

		//keep sampling stable
		delay_interval = time.Duration(interval)
		time.Sleep(delay_interval * time.Millisecond)
	}
}

func delayStates(last_delay [3]bool, index int, delay_interval time.Duration) (time.Duration, bool) {
	status := true
	// judge last delay status
	if index == 0 {
		return time.Duration(interval), true
	}

	idx := (index-1)%3
	status = last_delay[idx]

	for i:=0; i<3; i++ {
		if last_delay[index%3] {
			return time.Duration(interval), status
		}
	}
	delay_interval = delay_interval * 10
	if delay_interval > time.Duration(30000) {
		delay_interval = time.Duration(30000)
	}
	return delay_interval, status
}

func judge_delay(access [3]int, index int) bool {
	//return true
	sum := 0
	for i:=0; i<3; i++ {
		log.Debugf("[Cijitter] access is %d", access[i])
		sum += access[i]
	}
	mean := float64(sum)/3.0

	std := 0.0
	for i := 0; i < 3; i++ {
		std = std + (float64(access[i]) - mean) * (float64(access[i]) - mean)
    	}
	stddev := math.Sqrt(std)

	diff := 0
	ratio := 0.0
	count := 0.0
	if access[index] > access[(index+2)%3] {
		diff = access[index] - access[(index+2)%3]
		count = float64(diff)/float64(access[(index+2)%3])
	} else {
		diff = access[(index+2)%3] - access[index]
		count = float64(diff)/float64(access[(index+2)%3])
	}
	ratio = stddev/mean

	if count <= 0.1 || ratio <= 0.2 || (ratio <= 0.35 && count <= 0.35) {
		if mean < 100.0 {
			return false
		}
		return true
	} else{
		return false
	}
}

//call kernel module to get target address
var basePath string = "/monitor/"
var logPath string = basePath + "log/targetAddrs.list"
var kernelPath string = basePath + "kernel/"

//call kernel module to get target address
func read_sample_logs() ([]string, map[string]int) {
	var addr_access map[string]int
    	addr_access = make(map[string]int)
	var addrs_order []string
	addr := "0x000000"
	access := 0

    	fp, err := os.Open(logPath)
    	if err != nil {
		log.Debugf("[Cijitter] read_sample_logs: open log file failed: %s", err)
		return addrs_order, addr_access
    	}
    	defer fp.Close()

    	data := make([]byte, 8)
    	var k int64
    	index := 0
    	loc := 0

    	for {
        	data = data[:cap(data)]

        	// read bytes to slice
        	n, err := fp.Read(data)
        	if err != nil {
            	if err == io.EOF {
                	break
            	}
            	break
        }

        data = data[:n]
	binary.Read(bytes.NewBuffer(data), binary.LittleEndian, &k)

	// get address
	if index % 3 == 0 {
		addr = fmt.Sprintf("0x%x", k)
		addrs_order = append(addrs_order, addr)
		loc = index + 2
	}
	// get access number of the address
	if index == loc {
		access = int(k)
		addr_access[addr] = access
	}
	index ++
    }

    return addrs_order, addr_access
}

func get_pid() []string {
	var pids []string

	command := "ps -aux | grep nobody | grep exe | grep -v grep"
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		log.Debugf("[Cijitter] get pid failed:", err, output)
		return pids
	}

	max_cpu := 0.0
	target_pid := "-1"
	items := strings.Split(string(output), "\n")
	for _, item := range items {
		result := strings.Join(strings.Fields(item)," ")
		datas := strings.Split(result, " ")

		if len(datas) == 1 {
			continue
		}

		pid := datas[1]
		cpu := datas[2]
		mem := datas[3]
		//rss := datas[5]
		time := datas[9]

		if mem != "0.0" || cpu != "0.0" || time != "0:00" {
			cpu_data, _ := strconv.ParseFloat(cpu, 64)
			if cpu_data > max_cpu {
				max_cpu = cpu_data
				target_pid = pid
			}
		}
	}

	if target_pid != "-1" {
		pids = append(pids, target_pid) 
	}

	return pids
}

var DBGFS string ="/sys/kernel/debug/mapia/"
var DBGFS_ATTRS string = DBGFS + "attrs"
var DBGFS_PIDS string = DBGFS + "pids"
var DBGFS_TRACING_ON string = DBGFS + "tracing_on"

func chk_prerequisites() bool {
	// save old log file
	logf, err := os.Stat(logPath)
	if err == nil && !logf.IsDir(){
		os.Rename(logPath, logPath + ".old")
	} else {
		log.Debugf("[Cijitter] delete old log failed: %s", err)
	}

	// check kernel module
	kernel, err_kernel := os.Stat(DBGFS)
	if err_kernel != nil || !kernel.IsDir() {
		command := "cd " + kernelPath + " && sudo insmod daptrace.ko"
		cmd := exec.Command("bash", "-c", command)
		output, err := cmd.Output()
		if err != nil {
			log.Debugf("[Cijitter] kernel module load faild: %s, %s", err, output)
			return false
		}
	}

	pids, err_pids := os.Stat(DBGFS_PIDS)
	if err_pids != nil || pids.IsDir() {
		log.Debugf("[Cijitter] kmapia pids file not exists: %s", err_pids)
		return false
	}

	return true
}

func exit_handler() bool {
	command := "sudo rmmod daptrace"
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		log.Debugf("[Cijitter] rmmod kernel module failed:", err, output)
		return false
	}

	return true
}

func get_target_addr() (string, int, bool) {
	addr := ""
	access := -1
	targets := get_pid()
	if len(targets) == 0 {
		log.Debugf("[Cijitter] CANNOT GET TARGET PID...")
		return addr, access, false
	}

    	// strat kernel module
    	for _, pid := range targets {
		stat := chk_prerequisites()
		if !stat {
			return addr, access, false
		}

		command := "sudo echo " + pid + " > " + DBGFS_PIDS
		cmd := exec.Command("bash", "-c", command)
		cmd.Output()

		command = "sudo echo on > " + DBGFS_TRACING_ON
		cmd = exec.Command("bash", "-c", command)
		cmd.Output()

		// sampling duration
		time.Sleep(100 * time.Millisecond) // 0.1 seconds

		command = "sudo echo off > " + DBGFS_TRACING_ON
		cmd = exec.Command("bash", "-c", command)
		cmd.Output()

		if !exit_handler() {
			break
		}

		// get the target addr
		addr_order, addrs_access := read_sample_logs()
		if len(addr_order) == 0 {
			return addr, access, false
		}

		return addr_order[0], addrs_access[addr_order[0]], true
	}

	return addr, access, false
}
