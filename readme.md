# 用go写了一个压力测试报告,用于测试4090显示上跑ollama 

## 直接用deepseek 生成测试工具代码
### 提示词
```
用go写一个压力测试报告,用于测试4090显示上跑ollama deepseek-r1 1.5b 7b 14b 32b 在1、2、3、4、5、6个并发下的CPU、GPU、内容、回答时间 、并发达成与否等的表现
可以用:提示词语:
1、你好
2、三角函数是什么
3、用HTML写一个简单的webgl 三角型 3D 程序
最后输出一下表格
```
## 测试步骤
1. 安装ollama
2. ollama pull deepseek-r1:1.5b deepseek-r1:7b  ...
3. 编译程序 (windows 测试)
```
 go mod tidy 
 go build -o test.exe .
```
4. 运行程序 ./test.exe 或 go run .

3. 编译程序 (ubuntu 测试)
```
 go mod tidy 
 go build -o test .
```
4. 运行程序 ./test 或 go run .


