package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type TestResult struct {
	Model           string
	Concurrency     int
	CPULoad         float64
	GPULoad         float64
	GPUMemoryUsed   float64
	MemoryUsed      float64
	AvgResponseTime float64
	MaxResponseTime float64
	MinResponseTime float64
	SuccessRate     float64
}

type ResourceMetrics struct {
	CPULoad       float64
	GPULoad       float64
	GPUMemoryUsed float64
	MemoryUsed    float64
}

const (
	testDuration   = 30 * time.Second
	apiEndpoint    = "http://localhost:11434/api/generate"
	requestTimeout = 60 * time.Second
	coolDownPeriod = 10 * time.Second
)

var prompts = []string{
	"你好",
	"三角函数是什么",
	"用HTML写一个简单的webgl 三角型 3D 程序",
}

func main() {
	models := []string{
		"deepseek-r1:1.5b",
		"deepseek-r1:7b",
		"deepseek-r1:8b",
		"deepseek-r1:14b",
		"deepseek-r1:32b",
	}

	concurrencies := []int{1, 2, 3, 4, 5, 6}

	var results []TestResult

	for _, model := range models {
		for _, concurrency := range concurrencies {
			fmt.Printf("正在测试模型: %s, 并发数: %d\n", model, concurrency)
			result := runTest(model, concurrency)
			results = append(results, result)
			time.Sleep(coolDownPeriod)
		}
	}

	printResults(results)
}

func runTest(model string, concurrency int) TestResult {
	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var (
		mu              sync.Mutex
		totalRequests   int
		successCount    int
		responseTimes   []time.Duration
		resourceMetrics []ResourceMetrics
	)

	// 资源监控
	monitorCtx, stopMonitor := context.WithCancel(context.Background())
	defer stopMonitor()
	metricsChan := startMonitoring(monitorCtx)

	// 结果收集
	go func() {
		for metric := range metricsChan {
			mu.Lock()
			resourceMetrics = append(resourceMetrics, metric)
			mu.Unlock()
		}
	}()

	client := &http.Client{Timeout: requestTimeout}
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					prompt := prompts[rand.Intn(len(prompts))]
					duration, err := sendRequest(i, client, model, prompt)

					mu.Lock()
					totalRequests++
					if err == nil {
						successCount++
						responseTimes = append(responseTimes, duration)
					}
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	stopMonitor()

	// 计算统计指标
	avg, max, min := calculateStats(responseTimes)
	successRate := 0.0
	if totalRequests > 0 {
		successRate = float64(successCount) / float64(totalRequests) * 100
	}

	// 获取资源使用峰值
	maxMetrics := calculateMaxResources(resourceMetrics)

	return TestResult{
		Model:           model,
		Concurrency:     concurrency,
		CPULoad:         maxMetrics.CPULoad,
		GPULoad:         maxMetrics.GPULoad,
		GPUMemoryUsed:   maxMetrics.GPUMemoryUsed,
		MemoryUsed:      maxMetrics.MemoryUsed,
		AvgResponseTime: avg,
		MaxResponseTime: max,
		MinResponseTime: min,
		SuccessRate:     successRate,
	}
}

func sendRequest(idx int, client *http.Client, model, prompt string) (time.Duration, error) {
	start := time.Now()
	var response map[string]interface{}

	defer func() {
		if err := recover(); err != nil {
			fmt.Println("发生错误:", err)
		}
		if response["response"] != nil {
			fmt.Printf("[C-%d] [%s] [%s]请求耗时:%d  response size: %d\n",
				idx, model, prompt, time.Since(start), len(response["response"].(string)))
		} else {
			rsp := fmt.Sprintf("%+v", response)
			fmt.Printf("[C-%d] [%s] [%s]请求耗时:%d   response:\n%s\n", idx, model, prompt, time.Since(start), rsp)
		}
	}()

	requestBody, _ := json.Marshal(map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	})

	resp, err := client.Post(apiEndpoint, "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("非200状态码: %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, err
	}

	return time.Since(start), nil
}

func startMonitoring(ctx context.Context) <-chan ResourceMetrics {
	metricsChan := make(chan ResourceMetrics)
	go func() {
		defer close(metricsChan)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				cpuPercent, _ := cpu.Percent(0, false)
				memInfo, _ := mem.VirtualMemory()
				gpuUtil, gpuMem, _ := getGPUInfo()

				if len(cpuPercent) > 0 {
					metricsChan <- ResourceMetrics{
						CPULoad:       cpuPercent[0],
						MemoryUsed:    memInfo.UsedPercent,
						GPULoad:       gpuUtil,
						GPUMemoryUsed: gpuMem,
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return metricsChan
}

func getGPUInfo() (float64, float64, error) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,memory.used", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}

	fields := strings.Split(strings.TrimSpace(string(output)), ",")
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("invalid GPU data")
	}

	util, _ := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
	mem, _ := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)

	return util, mem, nil
}

func calculateStats(durations []time.Duration) (avg, max, min float64) {
	if len(durations) == 0 {
		return 0, 0, 0
	}

	var total time.Duration
	maxDur := durations[0]
	minDur := durations[0]

	for _, dur := range durations {
		total += dur
		if dur > maxDur {
			maxDur = dur
		}
		if dur < minDur {
			minDur = dur
		}
	}

	avgMs := total.Seconds() / float64(len(durations)) * 1000
	return avgMs, maxDur.Seconds() * 1000, minDur.Seconds() * 1000
}

func calculateMaxResources(metrics []ResourceMetrics) ResourceMetrics {
	max := ResourceMetrics{}
	for _, m := range metrics {
		if m.CPULoad > max.CPULoad {
			max.CPULoad = m.CPULoad
		}
		if m.GPULoad > max.GPULoad {
			max.GPULoad = m.GPULoad
		}
		if m.GPUMemoryUsed > max.GPUMemoryUsed {
			max.GPUMemoryUsed = m.GPUMemoryUsed
		}
		if m.MemoryUsed > max.MemoryUsed {
			max.MemoryUsed = m.MemoryUsed
		}
	}
	return max
}

func printResults(results []TestResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "模型\t并发数\tCPU负载(%)\tGPU负载(%)\t显存使用(MB)\t内存使用(%)\t平均响应(ms)\t最大响应(ms)\t最小响应(ms)\t成功率(%)\t")

	for _, r := range results {
		fmt.Fprintf(w, "%s\t%d\t%.1f\t%.1f\t%.0f\t%.1f\t%.1f\t%.1f\t%.1f\t%.1f\t\n",
			r.Model,
			r.Concurrency,
			r.CPULoad,
			r.GPULoad,
			r.GPUMemoryUsed,
			r.MemoryUsed,
			r.AvgResponseTime,
			r.MaxResponseTime,
			r.MinResponseTime,
			r.SuccessRate,
		)
	}

	w.Flush()
}
