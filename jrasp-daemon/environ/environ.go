package environ

import (
	"errors"
	"fmt"
	"jrasp-daemon/defs"
	"jrasp-daemon/utils"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

var (
	BuildDateTime   = ""
	BuildGitBranch  = ""
	BuildGitCommit  = ""
	BuildDecryptKey = ""
)

const (
	HOST_NAME      = "hostname.txt"
	HOST_NAME_PERM = 0777
)

const GB = 1024 * 1024 * 1024

type Environ struct {
	InstallDir  string `json:"installDir"`  // 安装目录
	HostName    string `json:"hostName"`    // 主机/容器名称
	Ip          string `json:"ip"`          // ipAddress
	OsType      string `json:"osType"`      // 操作系统类型
	BinFileName string `json:"binFileName"` // 磁盘可执行文件名
	BinFileHash string `json:"binFileHash"` // 磁盘可执行文件的md5值

	// 系统信息
	TotalMem  uint64 `json:"totalMem"`  // 总内存 GB
	CpuCounts int    `json:"cpuCounts"` // logic cpu cores
	FreeDisk  uint64 `json:"freeDisk"`  // 可用磁盘空间 GB
	Version   string `json:"version"`   // rasp 版本

	// 编译信息
	BuildDateTime  string `json:"buildDateTime"`
	BuildGitBranch string `json:"buildGitBranch"`
	BuildGitCommit string `json:"buildGitCommit"`

	// 密钥版本, 密钥最好是固化到代码中，golang二进制文件不太容易反编译，当需要更新密钥时，更新整个daemon即可
	// 必须为16位
	// 安全原因：不支持配置
	BuildDecryptKey string `json:"buildDecryptKey"`

	PidFile string `json:"pidFile"`

	// 是否为容器环境
	IsContainer bool `json:"isContainer"`
	// 是否已连接server
	IsConnectServer bool `json:"isConnectServer"`
}

func NewEnviron() (*Environ, error) {
	// 可执行文件路径
	execPath, err := filepath.Abs(os.Args[0])
	if err != nil {
		return nil, err
	}

	// install dir
	execDir := filepath.Dir(filepath.Dir(execPath))

	// md5 值
	md5Str, err := utils.GetFileMd5(execPath)
	if err != nil {
		return nil, err
	}
	// 可执行文件名
	execName := filepath.Base(execPath)
	// ip
	ipAddress, err := GetDefaultIp()
	if err != nil {
		return nil, err
	}

	// mem
	memInfo, _ := mem.VirtualMemory()

	// disk
	FreeDisk, err := GetInstallDisk(execDir)

	// cpu cnt
	cpuCounts, err := cpu.Counts(true)

	// .dockerenv
	isContainer, err := utils.PathExists("/.dockerenv")

	env := &Environ{
		HostName:        getHostname(execDir),
		Ip:              ipAddress,
		InstallDir:      execDir,
		OsType:          runtime.GOOS,
		BinFileHash:     md5Str,
		BinFileName:     execName,
		TotalMem:        memInfo.Total / GB,
		CpuCounts:       cpuCounts,
		FreeDisk:        FreeDisk,
		Version:         defs.JRASP_DAEMON_VERSION,
		BuildGitBranch:  BuildGitBranch,
		BuildDateTime:   BuildDateTime,
		BuildDecryptKey: BuildDecryptKey,
		BuildGitCommit:  BuildGitCommit,
		PidFile:         filepath.Join(execDir, defs.DAEMON_PID_FILE),
		IsContainer:     isContainer,
		IsConnectServer: false,
	}
	return env, nil
}

func getHostname(execDir string) string {
	hostName := ""

	// 从磁盘读取主机名
	hostFile := filepath.Join(execDir, "config", HOST_NAME)
	if hostNameFromFile, err := readHostNameFromFile(hostFile); err == nil {
		hostName = hostNameFromFile
	}

	// 如果磁盘上没有主机名，则从环境变量或系统获取
	if hostName == "" {
		hostName, _ = os.Hostname()
	}

	// 将主机名写入磁盘
	if err := writeHostNameToFile(hostFile, hostName); err != nil {
		fmt.Errorf("write hostname to file error:%v", err)
	}

	return hostName
}

func readHostNameFromFile(hostFile string) (string, error) {
	existed, err := utils.PathExists(hostFile)
	if !existed {
		return "", err
	}
	b, err := os.ReadFile(hostFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func writeHostNameToFile(hostFile string, hostName string) error {
	if err := os.MkdirAll(filepath.Dir(hostFile), HOST_NAME_PERM); err != nil {
		return err
	}
	return os.WriteFile(hostFile, []byte(hostName), HOST_NAME_PERM)
}

func isContainer() bool {
	isContainer, _ := utils.PathExists("/.dockerenv")
	return isContainer
}

func getExternalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", errors.New("are you connected to the network?")
}

func GetDefaultIp() (string, error) {
	conn, err := net.Dial("udp", "114.114.114.114:53")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	ip := strings.Split(localAddr.IP.String(), ":")[0]
	return ip, nil
}

func GetDefaultIface() (*net.Interface, error) {
	defaultIP, err := GetDefaultIp()
	if err != nil {
		return nil, err
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, item := range ifaces {
		addrs, err := item.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			ip := addr.(*net.IPNet)
			if ip.IP.String() == defaultIP {
				return &item, nil
			}
		}
	}
	return nil, errors.New("not found default ifaces")
}

func GetInstallDisk(path string) (free uint64, err error) {
	state, err := disk.Usage(path)
	if err != nil {
		return 0, err
	}
	free = state.Free / GB
	return free, nil
}
