# Prompt for Next Claude: 完成Task 005-02剩余工作

## 任务背景
你需要完成AIBrix项目中Task 005-02（完整性测试框架）的最后10%工作。前面的Claude已经修复了所有编译错误，现在需要验证测试运行并完成文档更新。

## 开始工作指南

### 1. 首先了解当前状态（5分钟）
```bash
# 阅读最新的交接文档，了解已完成的工作
cat kv_pub_sub_tasks/005-02-final-handover.md

# 查看任务要求
cat kv_pub_sub_tasks/005-02-completeness-testing.md

# 查看当前实现状态
cat kv_pub_sub_tasks/005-02-implementation-summary.md
```

### 2. 验证编译状态（10分钟）
```bash
# 验证所有测试都能编译
go test -tags="zmq" -c ./test/e2e && echo "✓ E2E编译成功"
go test -tags="zmq" -c ./test/benchmark && echo "✓ Benchmark编译成功"
go test -tags="zmq" -c ./test/chaos && echo "✓ Chaos编译成功"
```

### 3. 运行测试验证功能（30分钟）

#### 3.1 运行单元测试和集成测试
```bash
# 先运行基础的KV同步测试
make test-zmq
make test-kv-sync
```

#### 3.2 运行E2E测试
```bash
# 运行简化版E2E测试（应该能通过）
go test -tags="zmq" -v ./test/e2e/kv_sync_e2e_simple_test.go ./test/e2e/kv_sync_helpers.go ./test/e2e/util.go

# 运行完整E2E测试
make test-kv-sync-e2e
```

#### 3.3 运行性能测试
```bash
# 运行基准测试
make test-kv-sync-benchmark

# 或者单独运行
go test -bench=. -benchmem -benchtime=10s -tags="zmq" ./test/benchmark/
```

#### 3.4 运行混沌测试（如果环境支持）
```bash
# 检查是否安装了Chaos Mesh
kubectl get ns chaos-mesh

# 如果已安装，运行混沌测试
make test-kv-sync-chaos
```

### 4. 处理测试失败（如需要）

如果测试失败，可能的原因：
1. **环境问题**：确保安装了libzmq3-dev
2. **Mock问题**：某些测试可能需要真实的Kubernetes环境
3. **超时问题**：调整测试超时时间

常见修复方法：
```go
// 如果需要跳过某些需要真实环境的测试
if testing.Short() {
    t.Skip("Skipping test in short mode")
}

// 如果需要mock某些功能
// 参考test/e2e/kv_sync_e2e_simple_test.go的实现
```

### 5. 更新文档（10分钟）

#### 5.1 更新实现总结
编辑 `kv_pub_sub_tasks/005-02-implementation-summary.md`：
- 将进度从85%改为100%
- 更新"Current Status"部分
- 添加测试运行结果

#### 5.2 创建测试报告（可选）
如果需要，创建一个测试运行报告：
```bash
# 创建测试报告
cat > kv_pub_sub_tasks/005-02-test-report.md << EOF
# Task 005-02 Test Report

## Test Results
- Unit Tests: [PASS/FAIL]
- Integration Tests: [PASS/FAIL]
- E2E Tests: [PASS/FAIL]
- Performance Tests: [PASS/FAIL]
- Chaos Tests: [PASS/FAIL if applicable]

## Performance Metrics
[Include benchmark results]

## Issues Found
[List any issues]

## Recommendations
[Any recommendations for future work]
EOF
```

### 6. 验证CI/CD（如果有权限）

```bash
# 查看GitHub Actions工作流
ls -la .github/workflows/

# 主要工作流：
# - complete-testing.yml
# - nightly-performance.yml
# - kv-event-sync-tests.yml
```

## 成功标准

任务完成的标志：
1. ✅ 所有测试编译通过（已完成）
2. ✅ 核心测试运行通过
3. ✅ 文档更新完成
4. ✅ 创建了测试报告（如需要）

## 重要提示

1. **所有测试必须使用`-tags="zmq"`编译标签**
2. **如果某些测试需要真实Kubernetes环境，可以跳过并在报告中说明**
3. **重点确保核心功能测试通过，混沌测试可选**
4. **如果遇到环境问题，专注于验证代码正确性即可**

## 快速开始命令

```bash
# 一键运行所有测试（如果Makefile配置正确）
make test-kv-sync-all

# 如果上面失败，分步运行
make test-zmq && \
make test-kv-sync && \
make test-kv-sync-e2e && \
echo "核心测试完成"
```

## 预计时间
- 总时间：1-2小时
- 如果一切顺利：30分钟
- 如果需要调试：2小时

祝你顺利完成Task 005-02！记住，代码修复已经完成，你主要是验证和收尾工作。