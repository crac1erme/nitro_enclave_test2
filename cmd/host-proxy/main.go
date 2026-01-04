package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/mdlayher/vsock"
	"gopkg.in/yaml.v3"
)

type Config struct {
	VSOCK struct {
		ServerPort   int           `yaml:"server_port"`
		MaxWorkers   int           `yaml:"max_workers"`
		ReadTimeout  time.Duration `yaml:"read_timeout"`
		WriteTimeout time.Duration `yaml:"write_timeout"`
	} `yaml:"vsock"`
}

// 加载YAML配置文件
func loadConfig(filePath string) (*Config, error) {
	// 读取配置文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析YAML
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析YAML配置失败: %w", err)
	}

	// 配置校验 & 设置默认值
	if config.VSOCK.ServerPort <= 0 || config.VSOCK.ServerPort > 65535 {
		config.VSOCK.ServerPort = 10000 // 默认端口
		log.Printf("端口配置无效，使用默认值: %d", config.VSOCK.ServerPort)
	}
	if config.VSOCK.MaxWorkers <= 0 {
		config.VSOCK.MaxWorkers = 100 // 默认最大并发数
	}
	if config.VSOCK.ReadTimeout <= 0 {
		config.VSOCK.ReadTimeout = 30 * time.Second // 默认读超时
	}
	if config.VSOCK.WriteTimeout <= 0 {
		config.VSOCK.WriteTimeout = 30 * time.Second // 默认写超时
	}

	return &config, nil
}

// 处理Enclave连接（增加超时配置）
func handleEnclaveRequest(conn net.Conn, readTimeout, writeTimeout time.Duration) {
	defer conn.Close()

	// 类型断言为vsock.Conn，设置超时
	vsockConn, ok := conn.(*vsock.Conn)
	if !ok {
		log.Printf("非VSOCK连接，跳过")
		return
	}

	// 设置读写超时
	if err := vsockConn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		log.Printf("设置读超时失败: %v", err)
		return
	}
	if err := vsockConn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		log.Printf("设置写超时失败: %v", err)
		return
	}

	// 读取并响应请求
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		req := scanner.Text()
		log.Printf("收到Enclave请求：%s", req)

		// 模拟业务逻辑响应
		resp := fmt.Sprintf("宿主机已处理请求：%s | 宿主机CID=3", req)
		_, err := conn.Write([]byte(resp + "\n"))
		if err != nil {
			log.Printf("响应Enclave失败: %v", err)
			return
		}

		// 重置超时（避免长连接超时）
		vsockConn.SetReadDeadline(time.Now().Add(readTimeout))
	}

	if err := scanner.Err(); err != nil {
		log.Printf("读取Enclave数据失败: %v", err)
	}
}

func main() {
	// 获取本地VSOCK上下文
	ctx, err := vsock.ContextID()
	if err != nil {
		panic(err)
	}
	fmt.Printf("Local VSOCK Context ID (CID): %d\n", ctx)
	// 验证是否为父实例CID=3
	if ctx == 3 {
		fmt.Println("This is the AWS Nitro parent instance (CID=3)")
	}

	// 加载配置文件
	config, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("配置加载成功 | 监听端口: %d | 最大并发数: %d",
		config.VSOCK.ServerPort, config.VSOCK.MaxWorkers)

	// 转换配置为uint32（匹配ListenContextID的参数类型）
	//listenCID, _ := vsock.ContextID() //nitro enclave连宿主机用cid3
	listenCID := uint32(0)
	listenPort := uint32(config.VSOCK.ServerPort)

	listener, err := vsock.ListenContextID(listenCID, listenPort, nil)
	if err != nil {
		log.Fatalf("宿主机监听VSOCK失败: %v", err)
	}
	defer listener.Close()

	log.Printf("宿主机VSOCK服务端启动：CID=%d（监听所有），端口=%d",
		listenCID, listenPort)

	// 初始化工作池（限制并发数）
	workerPool := make(chan net.Conn, config.VSOCK.MaxWorkers)
	var wg sync.WaitGroup

	// 启动固定数量的工作goroutine
	for i := 0; i < config.VSOCK.MaxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for conn := range workerPool {
				handleEnclaveRequest(conn, config.VSOCK.ReadTimeout, config.VSOCK.WriteTimeout)
			}
		}()
	}

	// 循环接受连接
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("接受连接失败: %v", err)
			continue
		}
		log.Printf("收到Enclave连接：%s", conn.RemoteAddr())

		// 将连接放入工作池（满了会阻塞，避免goroutine泛滥）
		workerPool <- conn
	}

	//优雅退出逻辑（实际可通过信号量触发）
	//close(workerPool)
	//wg.Wait()
}
