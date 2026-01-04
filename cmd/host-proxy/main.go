package main

import (
	"fmt"

	"github.com/mdlayher/vsock"
)

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
}
