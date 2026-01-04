package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mdlayher/vsock"
	"gopkg.in/yaml.v3"
)

// 客户端配置结构体（YAML映射）
type ClientConfig struct {
	VSOCK struct {
		HostCID        int           `yaml:"host_cid"`        // 宿主机CID（Nitro固定为3）
		HostPort       int           `yaml:"host_port"`       // 宿主机监听端口
		ConnectTimeout time.Duration `yaml:"connect_timeout"` // 连接超时
		ReadTimeout    time.Duration `yaml:"read_timeout"`    // 读超时
		WriteTimeout   time.Duration `yaml:"write_timeout"`   // 写超时
	} `yaml:"vsock"`
}

// 加载客户端YAML配置
func loadClientConfig(filePath string) (*ClientConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取客户端配置失败: %w", err)
	}

	var config ClientConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析客户端配置失败: %w", err)
	}

	// 配置校验 & 默认值
	if config.VSOCK.HostCID <= 0 {
		config.VSOCK.HostCID = 3 // Nitro宿主机固定CID=3
		log.Printf("HostCID配置无效，使用默认值（Nitro宿主机）: %d", config.VSOCK.HostCID)
	}
	if config.VSOCK.HostPort <= 0 || config.VSOCK.HostPort > 65535 {
		config.VSOCK.HostPort = 10000 // 匹配服务端默认端口
		log.Printf("HostPort配置无效，使用默认值: %d", config.VSOCK.HostPort)
	}
	if config.VSOCK.ConnectTimeout <= 0 {
		config.VSOCK.ConnectTimeout = 5 * time.Second
	}
	if config.VSOCK.ReadTimeout <= 0 {
		config.VSOCK.ReadTimeout = 30 * time.Second
	}
	if config.VSOCK.WriteTimeout <= 0 {
		config.VSOCK.WriteTimeout = 30 * time.Second
	}

	return &config, nil
}

// 发送请求到宿主机并接收响应
func sendRequest(conn *vsock.Conn, reqMsg string, readTimeout, writeTimeout time.Duration) (string, error) {
	// 设置写超时
	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return "", fmt.Errorf("设置写超时失败: %w", err)
	}

	// 发送请求（加换行符，匹配服务端scanner.Text()解析）
	_, err := conn.Write([]byte(reqMsg + "\n"))
	if err != nil {
		return "", fmt.Errorf("发送请求失败: %w", err)
	}
	log.Printf("已发送请求到宿主机: %s", reqMsg)

	// 设置读超时
	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return "", fmt.Errorf("设置读超时失败: %w", err)
	}

	// 读取响应（按换行符分割）
	resp, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	return resp, nil
}

// 带超时的Dial方法（替代DialContext）
func dialWithTimeout(hostCID, hostPort uint32, timeout time.Duration) (*vsock.Conn, error) {
	// 用通道实现超时控制
	connChan := make(chan *vsock.Conn, 1)
	errChan := make(chan error, 1)

	// 异步执行Dial（避免阻塞）
	go func() {
		conn, err := vsock.Dial(hostCID, hostPort, nil)
		if err != nil {
			errChan <- err
			return
		}
		connChan <- conn
	}()

	// 超时控制
	select {
	case conn := <-connChan:
		return conn, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("连接超时（%v）", timeout)
	}
}

func main() {
	// 1. 加载配置
	config, err := loadClientConfig("client_config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("客户端配置加载成功 | 宿主机CID: %d | 宿主机端口: %d",
		config.VSOCK.HostCID, config.VSOCK.HostPort)

	// 2. 转换参数类型（匹配vsock.Dial的uint32参数）
	hostCID := uint32(config.VSOCK.HostCID)
	hostPort := uint32(config.VSOCK.HostPort)

	// 3. 带超时连接宿主机VSOCK服务端（替代DialContext）
	conn, err := dialWithTimeout(hostCID, hostPort, config.VSOCK.ConnectTimeout)
	if err != nil {
		log.Fatalf("连接宿主机失败: %v", err)
	}
	defer func() {
		_ = conn.Close()
		log.Println("客户端连接已关闭")
	}()
	log.Println("成功连接到宿主机VSOCK服务端")

	// 4. 处理信号（优雅退出）
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("收到退出信号，关闭客户端...")
		_ = conn.Close()
		os.Exit(0)
	}()

	// 5. 交互式发送请求（从标准输入读取）
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("===== 输入消息发送到宿主机（输入exit退出） =====")
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		reqMsg := scanner.Text()
		if reqMsg == "exit" {
			log.Println("用户输入exit，退出客户端")
			break
		}

		// 发送请求并获取响应
		resp, err := sendRequest(conn, reqMsg, config.VSOCK.ReadTimeout, config.VSOCK.WriteTimeout)
		if err != nil {
			log.Printf("请求失败: %v", err)
			continue
		}

		// 打印响应
		fmt.Printf("< 宿主机响应: %s", resp)
	}

	// 检查输入错误
	if err := scanner.Err(); err != nil {
		log.Fatalf("读取标准输入失败: %v", err)
	}
}
