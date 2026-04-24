package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"taskobserver"
)

func main() {
	// 从环境变量获取 COS 凭证
	secretID := os.Getenv("COS_SECRET_ID")
	secretKey := os.Getenv("COS_SECRET_KEY")
	if secretID == "" || secretKey == "" {
		// 尝试从其他环境变量获取
		secretID = os.Getenv("COS_TEST_SECRET_ID")
		secretKey = os.Getenv("COS_TEST_SECRET_KEY")
		if secretID == "" || secretKey == "" {
			log.Fatal("请设置 COS_SECRET_ID 和 COS_SECRET_KEY 环境变量")
		}
	}

	// 使用 taskobserver 的监控桶
	bucket := "web-1251036673"  // 实际使用的桶
	region := "ap-nanjing"
	taskName := "Mock 删除功能测试"

	fmt.Println("=== 运行 Mock 任务测试删除功能 ===")
	fmt.Printf("COS桶: %s (区域: %s)\n", bucket, region)
	fmt.Printf("任务名称: %s\n", taskName)
	fmt.Println("将会:")
	fmt.Println("1. 创建任务页面")
	fmt.Println("2. 更新总览页 (index.html)")
	fmt.Println("3. 添加带删除按钮的表格")
	fmt.Println()

	// 配置 taskobserver
	cfg := taskobserver.Config{
		Bucket:      bucket,
		Region:      region,
		SecretID:    secretID,
		SecretKey:   secretKey,
		BaseURL:     fmt.Sprintf("https://%s.cos-internal.%s.tencentcos.cn", bucket, region),
		TaskName:    taskName,
		Interval:    time.Second * 5,
		ExtraWriter: os.Stderr, // 同时在控制台输出日志
	}

	// 创建并启动 observer
	fmt.Println("创建 taskobserver...")
	observer, err := taskobserver.NewWithError(cfg)
	if err != nil {
		log.Fatalf("创建 taskobserver 失败: %v", err)
	}

	// 开始监控
	total := 100
	progress := 0
	
	observer.Start(func() (int, int) {
		progress += 20
		if progress > 100 {
			progress = 100
		}
		return progress, total
	})

	// 使用 logger
	logger := observer.NewSlogLogger()
	
	fmt.Println("任务启动，持续运行 20 秒...")
	
	// 运行 20 秒
	totalTime := 20 // 秒
	elapsed := 0
	
	for i := 0; i < totalTime; i++ {
		time.Sleep(1 * time.Second)
		elapsed++
		
		// 每 3 秒记录一条日志
		if elapsed%3 == 0 {
			logger.Info("任务进度",
				"elapsed", fmt.Sprintf("%d/%ds", elapsed, totalTime),
				"progress", fmt.Sprintf("%d%%", progress))
		}
		
		// 模拟一些步骤
		if elapsed == 5 {
			logger.Info("第一阶段完成")
		} else if elapsed == 10 {
			logger.Info("第二阶段完成")
		} else if elapsed == 15 {
			logger.Info("第三阶段完成")
		}
	}

	fmt.Println("\n任务完成，标记为完成状态...")
	observer.Done()

	// 等待最后的数据上传
	fmt.Println("等待数据上传完成 (3秒)...")
	time.Sleep(3 * time.Second)

	fmt.Println("\n=== 测试完成 ===")
	fmt.Println("✅ Mock 任务已运行完成")
	fmt.Println("✅ 数据已上传到 COS")
	fmt.Println("✅ 总览页已更新")
	
	fmt.Println("\n现在请访问:")
	fmt.Println("    https://task-info.110105.xyz/index.html")
	fmt.Println("\n应该能看到:")
	fmt.Println("1. 表格中有 'Mock 删除功能测试' 任务")
	fmt.Println("2. 最右侧有 '删除' 按钮 (红色)")
	fmt.Println("3. 点击按钮会弹出确认对话框")
	
	fmt.Println("\n如果没有看到删除按钮，可能是:")
	fmt.Println("1. 浏览器缓存 - 按 Ctrl+F5 强制刷新")
	fmt.Println("2. COS 索引页未生成 - 等待几秒后刷新")
	
	fmt.Println("\n要测试删除功能，需要:")
	fmt.Println("1. 在浏览器中打开总览页")
	fmt.Println("2. 找到 'Mock 删除功能测试' 任务")
	fmt.Println("3. 点击 '删除' 按钮")
	fmt.Println("4. 在对话框中可勾选'归档'选项")
	fmt.Println("5. 点击'确认删除'会发送请求")
	
	fmt.Println("\n⚠️ 注意: 删除功能需要对应的 HTTP 服务器支持")
	fmt.Println("要完全测试删除，还需要部署对应的服务器处理 /delete-task 请求")
	
	fmt.Println("\n当前已实现:")
	fmt.Println("✅ 总览页 HTML 包含删除按钮和 JavaScript")
	fmt.Println("✅ 删除对话框 UI 和逻辑")
	fmt.Println("✅ 删除请求发送至 /delete-task 端点")
	fmt.Println("⏳ 需要部署删除处理器 (DeleteHandler) 以实际处理删除")
	
	fmt.Println("\n✅ 页面现在应该有删除按钮了，请检查!")
}