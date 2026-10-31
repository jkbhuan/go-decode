# `decode` — 示波器协议解码与时序测量包

纯 Go 实现，不依赖示波器厂商、VISA、SCPI 或 UI。输入是已经从示波器下载并换算成
**伏特/秒**的两路模拟波形，输出是**强类型 Go 结构体**：协议事务、所有有效时序候选、
光标坐标（`X1`/`X2`，秒）和可解释的诊断。JSON 序列化由上层自行决定。

首期覆盖：

- **I²C**：7-bit 地址、R/W、地址/数据 ACK/NACK、START、Repeated START、STOP、时钟拉伸、
  采集截断、10-bit 地址前缀的显式 unsupported 诊断；
- **SMI / MDIO Clause 22**：PRE、ST、OP、PHYAD、REGAD、TA、DATA，读/写方向，
  前导码抑制，多帧，非法/截断帧诊断；
- **Keysight 波形换算**：InfiniiVision 风格 10 字段 preamble 与 Infiniium
  （MXR/EXR）多字段 preamble 的前 10 个共同字段；BYTE/WORD/LONG、有符号/无符号、
  little/big endian 必须**显式声明**；
- **时序候选选择**：`First` / `Min` / `Max`，可按名称、方向、frame/byte/bit、phase 过滤。

> 本包不做 Pass/Fail 判定，不推断“寄存器偏移”，不把 MDIO 高电平当作高阻的证明。

---

## 1. 安装与测试

模块路径：

```text
github.com/jkbhuan/go-decode
```

安装：

```bash
go get github.com/jkbhuan/go-decode
```

使用文档（调用方式、强类型返回结构、demo 波形图）：

- [docs/index.md](docs/index.md)
- [docs/index.html](docs/index.html)（带样式，可直接用浏览器打开）
- 波形图：`docs/images/i2c_demo.png`、`docs/images/smi_demo.png`、
  `docs/images/keysight_bin_i2c.png`、`docs/images/tektronix_wfm_i2c.png`

```bash
go test ./...
go vet ./...
go test -run '^$' -bench . -benchtime=20x ./...
```

本机结果（Windows amd64，Go 1.26.4）：

```text
ok      github.com/jkbhuan/go-decode
ok      github.com/jkbhuan/go-decode/internal/testwave [no test files]
go vet ./...                          # 无输出
# 129 个测试用例全部通过，0 失败
BenchmarkExtractLogicTrace-12         664555 ns/op
BenchmarkDecodeI2C-12                7963350 ns/op
BenchmarkDecodeSMI-12                5651650 ns/op
BenchmarkFormatKeysightWaveform-12    600245 ns/op
```

`go test -race ./...` 需要 cgo 与 C 编译器。本机没有 `gcc`/`clang`，因此该命令在
当前环境无法执行（`cgo: C compiler "gcc" not found`），不是测试失败。安装了 MinGW
或 Visual Studio C 工具链的机器可以直接运行。

模糊测试种子通过，并各做了短时间 `-fuzz` 运行（2–3 s，无崩溃）：
`FuzzDecodeI2C`、`FuzzDecodeSMI`、`FuzzFormatKeysightWaveform`、
`FuzzParseIEEE4882DefiniteBlock`。

示例输出：

- `examples/i2c/example_waveform.png`
- `examples/smi/example_waveform.png`

示例程序默认打印强类型结果摘要；`-png` 生成波形图（仅 Go 标准库），`-json` 是
可选的**上层** `json.Marshal` 演示：

```bash
go run ./examples/i2c -png examples/i2c/example_waveform.png
go run ./examples/smi -png examples/smi/example_waveform.png
```

### 1.1 实机验证（2026-10-27，Keysight MXR604A）

使用只读工具 `cmd/keysight-waveform-check` 验证了 `FormatKeysightWaveform` 对真实
Keysight 数据的换算（只发查询，不改变采集/格式/光标设置）：

```bash
go run ./cmd/keysight-waveform-check -resource "TCPIP0::10.42.71.194::hislip0::INSTR" -max-points 200000 -outdir verify_out_mxr604a
```

实测结果：

```text
*IDN?      KEYSIGHT TECHNOLOGIES,MXR604A,MY62329152,11.40.00401
Source     CHAN2
Format     WORD
Type       INT
ByteOrder  LSBF
Points     80,000,000
Preamble   2,6,80000000,1,1.25E-10,-5.00000010928488E-03,0,
           1.27551E-04,1.10408E+00,0,...
XInc       125 ps       (8 GSa/s)
XOrigin    -5.00000010928488 ms
XRef/YRef  0 / 0
YInc       1.27551E-04 V
YOrigin    1.10408 V
Encoding   WORD, signed, LSBF
Subset     200,000 points / 400,000 bytes (from the 80M-point record)
Converted  samples=200000 start=-5.00000010928 ms rate=8 GSa/s
Voltage    min=2.44591652 V max=2.56900323 V mean=2.51001199 V
Checks     all PASS (payload size, points, sample rate, start time,
           finite samples, monotonic time axis, preamble cross-fields)
```

说明：

- 该 MXR 不响应 `:WAVeform:UNSigned?`（InfiniiVision 专用），工具按 Infiniium 规则
  判定 WORD 为有符号，未触发超时后的会话失步。
- 子系统只下载了 80M 点记录的前 200,000 点。MXR Programmer's Guide 示例中
  `Start = Round((lTime - xOrg)/xInc); if (Start < 1) Start = 1;`，确认 `start=1`
  对应第一个数据点；因此该子集的 `StartTime` 取 `XOrigin` 是成立的。
- 本次未冻结采集（没有执行 STOP/`:DIGitize`），读取的是当时当前记录；要做可复现
  验收应由外层控制层先冻结采集。读取到的是 `INT`（插值）数据，适合验证换算，
  不用于时序/协议验收。
- 证据文件：`verify_out_mxr604a/{preamble.txt,settings.txt,payload.bin}`。

---

## 1.2 模块边界与数据来源

依赖方向（箭头表示导入）：

```text
decode (facade: decode.I2C / decode.SMI / type aliases)
  ├── decode/bus/i2c ─┐
  ├── decode/bus/smi ─┴── decode/core
  └── decode/compat → decode/scope/{keysight,tektronix}
decode/fileio → decode/scope/{keysight,tektronix} → decode/core
cmd/keysight-i2c-decode → go-visa Query/WriteString/QueryBinaryBlock
cmd/tektronix-i2c-decode → go-visa Query/WriteString/QueryBinaryBlock
应用层/cmd → wavefile.Open → decode/fileio.Read → Keysight .bin / Tektronix .wfm
应用层/cmd → decode.I2C / decode.SMI
```

- `decode/core` 不导入总线、品牌、SCPI 或 VISA。
- `decode/bus/i2c`、`decode/bus/smi` 仅依赖 `decode/core` 和标准库。
- `decode/scope/keysight`、`decode/scope/tektronix` 只做 payload/文件格式解析，
  不发送 SCPI，不打开文件，不依赖总线包。
- `decode/fileio` 只在调用方提供的 `io.ReaderAt` 上做格式识别和路由。
- 实时 SCPI 不抽成公共采集包：Keysight 和 Tektronix 分别把完整流程写在
  `cmd/keysight-i2c-decode`、`cmd/tektronix-i2c-decode`，直接调用 `go-visa`
  的 `Query`/`WriteString`/`QueryBinaryBlock`，并打印命令和响应。
- `wavefile`（顶层包）负责打开保存文件；`decode` 只接收已打开的数据流。

四种输入路径：

```go
// 1) 调用方已经有两路 Waveform（内存/自定义来源）
res, err := decode.I2C(scl, sda, opts)

// 2) 实时 SCPI：具体代码在 cmd 示例中，直接使用 go-visa
s, err := visa.Open(ctx, "TCPIP0::10.42.71.194::hislip0::INSTR")
idn, err := s.Query(ctx, "*IDN?")
s.WriteString(ctx, ":WAVeform:SOURce CHAN1")
pre, err := s.Query(ctx, ":WAVeform:PREamble?")
payload, err := s.QueryBinaryBlock(ctx, ":WAVeform:DATA?")
// 用 keysight.ParsePreamble / FormatWaveform 得到 scl、sda
res, err := decode.I2C(scl, sda, opts)

// 3) 保存文件：wavefile 打开文件，decode/fileio 解析调用方提供的数据流
cap, err := wavefile.Open("capture.wfm", fileio.Options{ChannelHint: "CH1", MaxPoints: 10_000_000})

// 4) 上层已经持有字节流/ReaderAt：直接调用纯解析器
cap, err := fileio.Read(r, size, fileio.Options{ChannelHint: "CH1"})
```

Tektronix 示例使用 `TCPIP0::<addr>::inst0::INSTR`，完整命令序列见
`cmd/tektronix-i2c-decode/main.go`。

> 原始 SCPI `QueryBinaryBlock` 负载没有 Preamble/编码元数据，不能由
> `fileio.Read` 猜测还原；必须使用 `fileio.ReadRaw(payload, RawMetadata{...})`
> 显式提供 Preamble、signedness、字节序和通道。

### 1.3 兼容 API 对照

| 旧符号 | 新符号 | 状态 |
| --- | --- | --- |
| `decode.DecodeI2C` | `decode.I2C` | 保留为 Deprecated 转发 |
| `decode.DecodeSMI` | `decode.SMI` | 保留为 Deprecated 转发 |
| `decode.Waveform` / `Timing` / `Diagnostic` | `decode/core` 同名类型（根包别名） | 兼容 |
| `decode.ParseKeysightPreamble` | `keysight.ParsePreamble`（根包转发） | 兼容 |
| `decode.FormatKeysightWaveform` | `keysight.FormatWaveform`（根包转发） | 兼容 |
| `decode.FormatTektronixWaveform` | `tektronix.FormatWaveform`（根包转发） | 由 `ErrNotImplemented` 改为实际换算 |
| `decode.I2CResult.Frames/Timings/...` | 保持不变，并增加 `Transactions` 分组 | 兼容 |
| `decode.SMIResult.Timings` | 新增跨帧展开视图 | 兼容扩展 |

### 1.4 文件解码命令行示例

`cmd/i2c-file-decode` 接收两个已保存的波形文件，打印 I²C 事务、帧、字节、
全部时序候选和诊断，并生成 PNG。它不连接仪器，也不依赖 go-visa。

```bash
# Keysight MXR604A AG10 .bin（真实生产样例）
go run ./cmd/i2c-file-decode \
  -scl "生产环境测试/i2c-keysight/scl.bin" \
  -sda "生产环境测试/i2c-keysight/sda.bin" \
  -png decode/docs/images/keysight_bin_i2c.png

# Tektronix WFM#003 .wfm（真实生产样例）
go run ./cmd/i2c-file-decode \
  -scl "生产环境测试/i2c-tek/scl.wfm" \
  -sda "生产环境测试/i2c-tek/sda.wfm" \
  -scl-channel CH1 -sda-channel CH2 \
  -png decode/docs/images/tektronix_wfm_i2c.png
```

实测结果：Keysight `.bin` 得到 2 transactions、3 frames（`0x70 write 0x08`、
`0x30 write 0x03`、`0x30 read 0x15` NACK）和 159 个 timings；Tektronix
`.wfm` 得到 1 transaction、1 frame（`0x4D write`，数据 `0x00 0x05`）和
81 个 timings。对应波形图见
`docs/images/keysight_bin_i2c.png`、`docs/images/tektronix_wfm_i2c.png`。

主要参数：`-scl`/`-sda`（必填）、`-scl-channel`（默认 `Channel 1`）、
`-sda-channel`（默认 `Channel 2`）、`-logic-low`/`-logic-high`（默认
0.99/2.31 V）、`-vdd`（默认 3.3 V，`0` 关闭 I²C 时序）、`-segment`、
`-max-points`、`-png`。单通道文件找不到标签时会回退到该 segment 的唯一记录。

### 1.5 实时 SCPI 命令行示例

实时读取代码没有抽成公共包，分别完整写在两个 cmd 中，直接调用 go-visa 的
`Query`、`WriteString`、`QueryBinaryBlock`。运行时 stderr 会逐条打印
`SCPI TX` / `SCPI RX`。

```bash
# Keysight MXR/EXR：CHAN1=SCL, CHAN2=SDA
go run ./cmd/keysight-i2c-decode \
  -addr 10.42.71.194 \
  -window-start 1.25e-3 -window-duration 200e-6 \
  -logic-low 0.99 -logic-high 2.31 -vdd 3.3

# Tektronix 4/5/6-Series MSO：inst0::INSTR，CH1=SCL, CH2=SDA
go run ./cmd/tektronix-i2c-decode \
  -addr 192.168.1.100 \
  -scl-channel CH1 -sda-channel CH2 \
  -logic-low 0.99 -logic-high 2.31 -vdd 3.3
```

Keysight 示例使用 `:WAVeform:FORMat/BYTEorder/SOURce/PREamble?/POINts?/DATA?`；
Tektronix 示例依据 `4-5-6-Series-MSO_Programmer.pdf` 使用
`:DATa:SOUrce/ENCdg/WIDth/STARt/STOP`、`:HORizontal:RECOrdlength?`、
`:WFMOutpre?` 和 `:CURVe?`。

---

## 2. 数据单位与时间轴

- `Waveform.Samples`：伏特（V）。
- `Waveform.SampleRate`：Sa/s，必须为正有限值。
- `Waveform.StartTime`：**第 0 个采样点**的仪器水平时间（s），不要求从 0 开始，
  允许负值；**禁止**传 Unix epoch 秒。
- `Waveform.TimeAt(i) = StartTime + i/SampleRate`。
- `Timing.Value`、`Timing.X1`、`Timing.X2`：秒，且位于同一仪器时间轴。
  `X1 <= X2`，`Value == X2-X1`（浮点误差内）。`0` 是合法测量值，不等于“未测到”。
- 两通道按**时间戳**对齐，不按下标对齐；支持不同采样率、不同长度、不同 `StartTime`。
  部分重叠时裁剪到公共区间并输出 `waveform_partial_overlap`；无重叠返回错误。

`Waveform.Validate()` 检查数组非空、采样率/起点有限、所有采样值有限。
`TimeAt` 越界返回 `ErrIndexOutOfRange`，不会 panic。

---

## 3. I²C 使用示例

```go
res, err := decode.I2C(scl, sda, decode.I2COptions{
    SCLThreshold: decode.LogicThreshold{Low: 1.2, High: 2.1}, // 协议解码门限 + 迟滞
    SDAThreshold: decode.LogicThreshold{Low: 1.2, High: 2.1},
    VDD:          3.3,        // 仅用于推导时序参考电平 0.3*VDD / 0.7*VDD
    GlitchWidth:  50e-9,      // 可选毛刺过滤；0 表示关闭
})
if err != nil {
    // 配置错误/波形非法/无时间重叠等
}
for _, f := range res.Frames {
    // f.TransactionIndex, f.Address, f.Direction, f.AddressACK, f.Data
}
for _, tm := range res.Timings {
    // tm.Name, tm.Value(s), tm.X1(s), tm.X2(s), tm.FrameIndex, ...
}
for _, d := range res.Diagnostics {
    // d.Code, d.Message, d.Severity
}
```

### 3.1 Frame / Transaction 语义

- 一个 `I2CFrame` 是从 START 或 Repeated START 开始、到下一个 Repeated START 或
  STOP 为止的地址/数据阶段。
- 一个 `I2CTransaction` 是从 START 到 STOP 的所有 frame。组合读
  “写 + Repeated START + 读” 是 **一个 transaction、两个方向不同的 frame**，
  字节只作为原始数据报告，**没有** `Offset` 字段。
- `RepeatedStart`、`HasStop`、`Complete` 区分“正常结束”“被 Repeated START 中断”
  “采集截断”。`Complete` 表示协议内容完整且结束在字节边界，**不**表示设备 ACK；
  NACK 是完整解码结果。
- 第 9 位未采到时 `ACKUnknown`，同时输出 `i2c_missing_ack`。
- 10-bit 地址前缀 `11110xx` 不会被当作 7-bit：`AddressBits=0`、`Address=0`、
  `Direction=unknown`、`Complete=false`，并输出 `i2c_10bit_addressing_unsupported`。
- STOP/Repeated START 前会有一个用于建立条件的 SCL 上升沿；该沿不是数据位。
  解码器会丢弃紧随最后一个 ACK 之后的单个“条件建立”位，不把它当数据字节。
  若条件出现前已有 2 个以上数据位，则按截断处理并诊断。

### 3.2 I²C 时序定义与参考电平

依据 NXP **UM10204 Rev. 7 (2021-10-01)** Figure 38 与 Table 11
（Standard/Fast/Fast-mode Plus）。时序轨迹使用带迟滞的
`Low = 0.3*VDD`、`High = 0.7*VDD`：

| 名称 | 起点 → 终点 | 说明 |
| --- | --- | --- |
| `tHD;STA` | START/Repeated START 的 SDA 下降沿（30%）→ 随后 SCL 下降沿（30%） | 每次 START/Sr 独立测量 |
| `tSU;STA` | Repeated START 前的 SCL 上升沿（70%）→ Sr 的 SDA 下降沿（30%） | **只对 Repeated START 产生**；空闲总线后的首个 START 不填值 |
| `tSU;STO` | STOP 前最后一个 SCL 上升沿（70%）→ STOP 的 SDA 上升沿（70%） | 波形截断/无 SCL 上升则不产生 |
| `tBUF` | 前一 transaction 的 STOP SDA 上升沿（70%）→ 本 transaction 的 START SDA 下降沿（30%）（`X1`=前一 STOP，`X2`=本次 START） | 挂到**下一笔** transaction；无前 STOP 或无后 START 则不产生 |
| `tSU;DAT` | 前一 SCL 下降沿之后、本采样 SCL 上升沿之前的**最后一个可观测 SDA 转换** → SCL 上升沿（70%） | 只有 SDA 确实发生转换才产生；按 address/data/ack 标记 |
| `tHD;DAT` | SCL 下降沿（30%）→ 之后、下一采样 SCL 上升沿之前的**第一个可观测 SDA 转换** | 同样不臆测 |
| `tHIGH` | SCL 上升沿（70%）→ 下降沿（30%） | transaction 内每个完整高电平 |
| `tLOW` | SCL 下降沿（30%）→ 上升沿（70%） | 时钟拉伸自然体现为较长的 `tLOW` |
| `fSCL` | 相邻两次 SCL 上升沿 | 以 `I2CTransaction.SCLFrequencies`（Hz）单独输出；`Timing.Value` 始终是秒，不复用为频率 |

- 所有有效候选都保留在 `res.Timings`（以及按事务分组的 `tx.Timings`）/
  `res.SCLFrequencies`，不在解码阶段随机挑一个。
- 每个 `Timing` 带 `FrameIndex`、`ByteIndex`、`BitIndex`、`Phase`
  （`start`、`address`、`ack`、`data`、`stop`、`busfree`、`clock`）和 `Direction`。
  `ByteIndex=0` 是地址字节，数据字节从 1 开始；`BitIndex=8` 表示 ACK 位。
- 没有 `Timing` 的指标一定有 `Diagnostic{Code:"timing_not_observable"}` 说明原因；
  未配置时序门限时输出 `i2c_timing_threshold_unavailable`，不产生任何数值。
- **协议解码门限与时序门限分离**：`SCLThreshold`/`SDAThreshold` 只做逻辑解码；
  `TimingThreshold`（或由用户显式提供的 `VDD` 推导的 0.3/0.7 VDD）用于时序。
- 毛刺过滤 `GlitchWidth` 同时作用于解码轨迹与时序轨迹，且会在诊断中报告；
  它移除短脉冲而不是把交叉点悄悄平移。

---

## 4. SMI / MDIO Clause 22 使用示例

```go
res, err := decode.SMI(mdc, mdio, decode.SMIOptions{
    MDCThreshold:             decode.LogicThreshold{Low: 0.8, High: 2.0},
    MDIOThreshold:            decode.LogicThreshold{Low: 0.8, High: 2.0},
    TimingThreshold:          &decode.LogicThreshold{Low: 0.8, High: 2.0}, // 可选
    AllowPreambleSuppression: true, // 允许省略 32 个前导 1
    GlitchWidth:              50e-9,
})
for _, f := range res.Frames {
    // f.PHYAddr, f.RegAddr, f.Data, f.Direction, f.Turnaround
}
for _, tm := range res.Timings {
    // tm.Name, tm.Value, tm.X1, tm.X2, tm.FrameIndex
}
```

### 4.1 帧格式与解码规则

依据 IEEE Std 802.3-2022 Clause 22.2.4.5（Table 22-12）与 22.3.4：

```text
PRE(32x1，可抑制) | ST(01) | OP(2) | PHYAD(5) | REGAD(5) | TA(2) | DATA(16)
OP: 01 = Write, 10 = Read
TA: Write 10；Read Z0（波形上通常读为 10）
```

- 在 **MDC 上升沿**采样 MDIO，所有字段 MSB first。
- `PHYAddr` 与 `RegAddr` 分开输出；`DataValid` 明确区分“观测到 0”和“DATA 未收全”。
- `Complete=false` 表示 32 个帧位没有全部收到；此时不能把 `Data` 当作有效值。
- `BitsReceived` 给出 ST..DATA 实际收到的位数（不含 PRE）。
- Read 的 TA 第一位为高时 `Turnaround=StatusUnverifiable`：高电平与“PHY 释放总线、
  上拉为高”一致，但**仅凭模拟电压不能证明高阻**。给出
  `smi_read_turnaround_unverifiable`。
- Write TA 期望 `10`；否则 `StatusInvalid` + `smi_invalid_turnaround`。
- 非法 OP 输出 `smi_invalid_opcode`，`Direction=unknown`，但已观测到的 PHY/REG/DATA
  仍按位报告（不补造）。
- 前导码：`AllowPreambleSuppression=false` 时要求至少 `MinPreambleBits`（默认 32）个
  连续 1；为 `true` 时允许 0 个。`PreambleBits` 最多报告标准/配置长度（默认 32），
  多出的前导 1 视为空闲。
- 严格状态机：只有完整帧结束并回到 idle 后才寻找下一帧 ST；数据区内的 `01`
  不会触发新帧。帧中途的 START 候选不会补造字段。
- 仅开头收到 ST 的第一个 0、第二个 1 未到时，不输出帧，只给出
  `smi_truncated_frame` 诊断。

### 4.2 SMI 时序

| 名称 | 测量 | 方向 |
| --- | --- | --- |
| `MDCHigh` | MDC 在 Vih 门限上的上升沿 → 下降沿 | 所有帧（IEEE 802.3 22.3：≥Vih 的持续时间） |
| `MDCLow` | MDC 在 Vil 门限上的下降沿 → 上升沿 | 所有帧（≤Vil 的持续时间） |
| `MDCPeriod` | 相邻 MDC 上升沿（Vih） | 所有帧 |
| `tSU(MDIO)` | MDC 下降沿后/上升沿前的最后一个 MDIO 转换 → MDC 上升沿 | **仅 Write**（STA 驱动 MDIO） |
| `tHD(MDIO)` | MDC 上升沿 → 下一 MDC 上升沿前的第一个 MDIO 转换 | **仅 Write** |

- Read 方向**不复用** Write 的 tSU/tHD 定义。TA 之后 MDIO 由 PHY 驱动，其
  clock-to-output 关系属于器件参数（常见 `tVAL`/`tOINVLD`），Clause 22 不定义
  统一的 tSU/tHD。解码器输出 `smi_read_direction_timing_unsupported` 诊断，
  不给出臆测值；MDC 的通用周期/高低电平仍照常测量。
- 同样只对**可观测的 MDIO 转换**产生 Setup/Hold；连续相同位不产生假值。
- 时序门限由 `TimingThreshold` 显式给出。未给出时不产生 SMI 时序，并输出
  `timing_threshold_missing`。代码中**没有**把 MDIO 的电平硬编码为某个 PHY 的
  具体门限；IEEE 802.3 的 MII `Vih(min)=2.00 V`、`Vil(max)=0.80 V` 只作为示例。

---

## 5. 时序候选选择与光标

```go
tm, ok := decode.SelectTiming(res.Timings, decode.TimingFilter{
    Name:      decode.TimingSUDAT,
    Direction: decode.DirectionWrite, // unknown = 不过滤
    Phase:     "data",
}, decode.SelectMin)
if ok {
    // 仪器控制层使用 tm.X1 / tm.X2（秒）设置水平光标
}
```

- 先过滤后选择；没有候选返回 `false`，不返回伪造的空 timing。
- `First` = 最早 `X1`；`Min`/`Max` = 最小/最大 `Value`；并列时选最早 `X1`。
- `SelectTimings` 返回全部匹配候选。
- 光标回放属于仪器控制层：写 `X1/X2` 前必须确认当前仍是同一采集记录，且
  示波器光标命令的时间参考与 `Waveform.StartTime` 一致。不同 Keysight 型号的
  `:MARKer:` 命令可能不同，必须按 `*IDN?` 对应型号的 Programmer's Guide 验证。
- 回读光标值允许有仪器量化误差。

---

## 6. Keysight 波形换算

```go
p, info, err := decode.ParseKeysightPreamble(preambleText)
// info.Format 的编码因型号而异，不要跨型号解释。

payload, rest, err := decode.ParseIEEE4882DefiniteBlock(block)
// 若通信层已经剥离 IEEE 488.2 definite-length 头，可跳过这一步。

w, err := decode.FormatKeysightWaveform(payload, p, decode.SampleEncoding{
    BytesPerPoint: 2,
    Signed:        true,     // 必须按型号/格式显式声明
    ByteOrder:     "little", // LSBFirst；MSBFirst 用 "big"
})
```

换算公式：

```text
Voltage(i)  = (Raw(i) - YReference) * YIncrement + YOrigin
SampleRate  = 1 / XIncrement
StartTime   = XOrigin - XReference * XIncrement
Time(i)     = StartTime + i * XIncrement
```

必须校验：preamble 非有限值、`XIncrement<=0`、`YIncrement<=0`、负载长度不是
`BytesPerPoint` 的整数倍、未知字节序、非法 `BytesPerPoint`。

**型号差异（重要）**：

- InfiniiVision 3000 X-Series Programmer's Guide：`:WAVeform:PREamble?` 为 10 个字段；
  `XReference=0`；BYTE/WORD 的原始值有无符号取决于 `:WAVeform:UNSigned`（默认 ON）；
  BYTE 时 `YReference` 为 128（有符号时需相应理解）。
- Infiniium（MXR/EXR）Programmer's Guide：`:WAVeform:PREamble?` 返回 10 个共同字段
  **之后还有** coupling、显示范围、日期时间等字段；`XReference=0`、`YReference=0`；
  **BYTE 为有符号 8 位，WORD 为有符号 16 位**。本包的解析器接受并忽略额外字段，
  但 signedness 必须由调用方显式传入。
- `Format` 代码也不同（3000X：0=BYTE、1=WORD、4=ASCii；Infiniium：0=ASCii、
  1=BYTE、2=WORD…），所以 `KeysightPreambleInfo.Format` 只是原样报告，不用于
  自动决定编码。
- **Tektronix**：`scope/tektronix` 已实现：
  - 实时 SCPI：`WFMOutpre?` + `CURVe?`，按 `BYT_Nr/BN_Fmt/BYT_Or/NR_Pt/PT_Fmt/XINcr/
    XZERo/PT_Off/YMUlt/YOFf/YZERo` 换算，使用
    `V=((raw-YOff)*YMult)+YZero`、`t=XZero+(i-PTOffset)*XIncr`；
  - 保存文件：`WFM#002/#003` 普通 YT 向量，支持 LE/BE、有效采样区（pre/postcharge）
    和 int8/int16/int32/uint32/uint64/float32/float64；FastFrame/envelope 明确拒绝。
  - 不套用 Keysight 公式。真实 `.wfm` 样例已离线解码 I²C；实时 SCPI 代码在
    `cmd/tektronix-i2c-decode/main.go`，尚无泰克实机验证。
- **Holes**：各型号/格式用不同的原始值表示无效采样点（例如 3000X BYTE/WORD 的 0，
  Infiniium BYTE 的 125、WORD 的 32672）。本包不猜测 hole 策略，也不会把 hole 静默
  变成某个电压；调用方应使用无 hole 的新鲜采集，或先按具体型号文档处理 holes。
  `Waveform.Validate` 要求所有采样值有限，因此 hole 不应以 NaN 形式混入。

---

## 7. 诊断（Diagnostic）

`Diagnostic{Code, Message, Severity, Time}` 是普通 Go 结构体。配置/波形非法返回
Go `error`；采集截断、协议异常、不可测指标返回诊断并尽量保留已解析结果。`decode`
不做 JSON 序列化；需要时由上层 `json.Marshal`。

主要编码：

| Code | 含义 |
| --- | --- |
| `waveform_partial_overlap` | 两通道只部分重叠，已裁剪 |
| `waveform_no_time_overlap` | 无重叠（作为 error 返回） |
| `waveform_initial_level_unknown` | 记录起点在门限带内，电平直到 `KnownFrom` 才确定 |
| `waveform_signal_static` / `waveform_never_crosses_threshold` | 无有效边沿 |
| `waveform_sampling_low` | 最短脉冲不足 3 个采样间隔，边沿/脉宽精度受采样限制 |
| `edge_glitch_suppressed` | 毛刺过滤移除了短脉冲 |
| `timing_threshold_missing` / `i2c_timing_threshold_unavailable` | 未配置时域参考电平，故不输出时序 |
| `timing_not_observable` | 该指标缺边沿/缺转换/缺前驱后继，附带原因 |
| `i2c_no_start_condition` | 有 SCL 活动但无 START，可能是半帧开始，未补帧 |
| `i2c_missing_ack` / `i2c_missing_stop` / `i2c_truncated_byte` | 采集截断/缺 ACK/STOP |
| `i2c_10bit_addressing_unsupported` | 10-bit 地址前缀，不按 7-bit 解释 |
| `smi_preamble_short` | 前导码不足且不允许抑制 |
| `smi_invalid_start_of_frame` / `smi_invalid_opcode` / `smi_invalid_turnaround` | 帧字段异常 |
| `smi_read_turnaround_unverifiable` | 读 TA 高电平不能证明高阻 |
| `smi_truncated_frame` | 帧位/字段未收全 |
| `smi_read_direction_timing_unsupported` | 读方向不套用写方向 Setup/Hold |
| `smi_no_frames` | 未发现有效 Clause 22 帧 |

---

## 8. 分层结构

```text
decode/
├── decode.go          package 文档
├── waveform.go        Waveform、时间轴、验证、双通道裁剪
├── threshold.go       逻辑门限/迟滞
├── api.go             顶层 decode.I2C / decode.SMI 与类型别名
├── compat.go          旧 Keysight/Tektronix 符号的兼容转发
├── core/              Waveform、Capture、边沿/时序/诊断/对齐
├── bus/i2c/           I²C 状态机与 UM10204 时序
├── bus/smi/           MDIO Clause 22 状态机与时序
├── scope/             IEEE 488.2 块解析（无仪器 I/O）
│   ├── keysight/      preamble、payload、AG10 .bin 解析
│   └── tektronix/     WFMOutpre、CURVe? payload、WFM#002/#003 .wfm 解析
├── fileio/            io.ReaderAt 格式路由、raw payload（显式元数据）
├── internal/testwave/ 测试专用合成器
├── testdata/          人工黄金向量与格式样本
├── examples/          内存波形示例与 PNG
└── docs/              使用文档

# decode 包之外（所有仪器/文件 I/O）
wavefile/                    打开保存文件后交给 decode/fileio 解析
cmd/keysight-waveform-check/ 实机只读校验
cmd/keysight-i2c-decode/     Keysight VISA/SCPI → decode.I2C → PNG
cmd/tektronix-i2c-decode/    Tektronix VISA/SCPI（inst0::INSTR）→ decode.I2C → PNG
cmd/decode-file/             wavefile → decode.I2C/SMI
cmd/i2c-file-decode/         SCL/SDA 文件 → I²C 时序 + PNG
cmd/keysight-download/       旧的一次性原始负载下载脚本
internal/waveplot/           标准库 PNG 渲染
```

`decode/` 内不出现 `context`、SCPI 命令、`go-visa` 或 `os.Open`；解码只看
调用方传入的 `Waveform`、响应 payload、`io.ReaderAt` 或 `[]byte`。实时 SCPI
没有公共采集包，完整代码在 `cmd/keysight-i2c-decode` 和
`cmd/tektronix-i2c-decode`。

---

## 8.2 支持矩阵

| 路径 | 状态 | 证据/限制 |
| --- | --- | --- |
| I²C / SMI 纯算法 | 已实现并离线验证 | Golden Vectors、合成波形、模糊测试 |
| Keysight 实时 SCPI | 已实现；波形换算已实机验证（MXR604A） | 未做真实 I²C/MDIO 总线验收；`:MARKer:` 未验证 |
| Tektronix 实时 SCPI | 已实现（4/5/6-Series 手册命令） | `cmd/tektronix-i2c-decode` 编译通过；无实机 |
| Keysight 原生 `.bin` AG10 | 已实现并离线验证 | 真实 `scl.bin/sda.bin` 解码出 I²C；多通道/分段按 AG10 记录保留 |
| Tektronix 原生 `.wfm` WFM#002/#003 | 已实现并离线验证 | 真实 `scl.wfm/sda.wfm` 解码出 I²C；FastFrame/envelope 明确 unsupported |
| 原始 SCPI payload | 已实现（显式 Preamble+Encoding） | 无元数据时明确拒绝，不猜测 |
| Keysight 原生 `.wfm` | 明确不支持 | 文件头非 AG10；不猜测 |
| Tektronix CSV | 未实现 | 本轮不声明支持 |

## 9. 已知限制与待实机验证

已完成（纯模拟/离线）：

- 全部单元测试、人工黄金向量、异常/模糊种子测试通过；`go vet` 通过。
- I²C 普通写/读、组合读、多事务 `tBUF`、时钟拉伸、截断、10-bit 拒绝、噪声/慢边沿、
  毛刺过滤、不同采样率与时间偏移、负 `StartTime`。
- MDIO 写/读、TA 可验证性、前导码抑制、数据区 `01` 不误判、连续多帧、非法
  OP/TA/ST、截断、不同采样率与偏移。
- Keysight BYTE/WORD/LONG、signed/unsigned、LSB/MSB、XReference、块头解析。
- `go test -bench` 基础性能数据。

**尚未完成/待实机确认**：

1. Keysight MXR604A（`10.42.71.194`）的真实波形换算已完成验证（见 §1.1）；但尚未
   下载完整 80M 点记录、未验证 `:MARKer:` 光标命令及回读、未验证 InfiniiVision
   3000X 家族，也未处理真实记录中的 holes。
2. 真实 I²C/MDIO 信号源：无独立信号源。Keysight `.bin` 与 Tektronix `.wfm`
   真实样例已离线解码出 I²C 事务；实时 Keysight 读取已在 MXR604A 上验证波形换算，
   但尚未用可控 I²C/MDIO 源做协议验收。Tektronix 实时 SCPI 示例已按手册实现并通过编译，
   尚无泰克实机验证。
3. 未做 sigrok/libsigrokdecode 可选交叉验证；Keysight 原生 `.wfm` 与 Tektronix CSV
   明确不在本轮支持范围。
4. 未在支持 cgo 的机器上运行 `go test -race ./...`；当前环境缺 C 编译器。
5. 没有自动 Pass/Fail 判定、10-bit I²C、SMBus/PMBus、Clause 45、SPI/UART/CAN。
6. 若具体器件的 SMI 定义不是 IEEE 802.3 Clause 22（私有 SMI/其他帧格式），本 API
   不适用，应另设协议解码器；本包不会猜测厂商扩展。
7. 紧接最后一个 ACK 的单个 SCL 上升沿被当作 STOP/Repeated START 的条件建立位，
   不报告为数据位。若总线确实在 ACK 后只发送了 1 个数据位就被 STOP 中断，该帧可能
   被当作在字节边界正常结束；这是仅凭波形无法区分的边界情形。

实机验证建议顺序：`*IDN?` → 核对 Programmer's Guide → 下载一条真实模拟通道波形
与 preamble → 用探头补偿方波核对电压/边沿/光标 → 固定采集记录写入并回读 X1/X2 →
有真实总线时再做协议验收。

---

## 10. 参考规范

1. NXP UM10204, *I²C-bus specification and user manual*, Rev. 7 (2021-10-01)：
   Section 6 / Figure 38 / Table 11。
2. IEEE Std 802.3-2022, Clause 22（Management Data Input/Output）：Table 22-12、
   22.2.4.5、22.3.4、Figure 22-18/22-19、22.4.4.1。
3. Keysight *InfiniiVision 3000 X-Series Programmer's Guide*：`:WAVeform` 命令。
4. Keysight *Infiniium MXR/EXR Programmer's Guide*：`:WAVeform:PREamble?` 与
   `:WAVeform:DATA?`。
5. sigrok `i2c` / `mdio` 解码说明：仅用于行为参考与可选交叉验证，未复制 GPL 源码。
