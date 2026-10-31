# `decode` 模块化重构与多品牌波形接入 — 开发报告

- 版本：v2.1
- 日期：2026-10-28
- 模块：`github.com/jkbhuan/go-decode`
- 入口包：`github.com/jkbhuan/go-decode`
- 本轮依据：`Go_decode_模块化重构与多品牌波形接入_任务说明.md`

---

## 1. 阶段执行结果

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| P0 | 基线 `go test ./...` / `go vet ./...`、调用点梳理、Golden/合成波形保留 | 完成 |
| P1 | `decode/core`、`decode/bus/i2c`、`decode/bus/smi`、顶层 `decode.I2C/SMI`、旧 API 转发 | 完成 |
| P2 | `cmd/keysight-i2c-decode` 内直接写 VISA/SCPI 实时读取、设置恢复与冻结策略 | 完成（MXR 波形换算此前已实机验证） |
| P3 | `cmd/tektronix-i2c-decode` 实时 `WFMOutpre?` + `CURVe?` 读取、`WFM#002/#003` 文件解析 | 完成（按 4/5/6-Series 手册实现；文件真实样例离线验证；无泰克实机） |
| P4 | `fileio` 路由、Keysight `.bin` AG10、Tektronix `.wfm`、raw payload + 显式元数据、`cmd/decode-file`、`cmd/i2c-file-decode` | 完成 |
| P5 | 文档、支持矩阵、测试/静态检查、变更与遗留问题 | 完成（见下） |
| P6 | 边界修正：实时 SCPI 与文件打开移出 `decode/` | 完成 |

---

## 2. 架构与依赖

```text
decode（Facade：decode.I2C / decode.SMI / 类型别名；无仪器/文件 I/O）
  ├── decode/core         Waveform/Capture/边沿/时序/诊断
  ├── decode/bus/i2c      I²C 状态机 + UM10204 时序
  ├── decode/bus/smi      MDIO Clause 22 状态机 + 时序
  ├── decode/scope        IEEE 488.2 block 解析（纯数据）
  │   ├── keysight        preamble/payload/AG10 .bin 解析
  │   └── tektronix       WFMOutpre/CURVe? payload/WFM#002/#003 .wfm 解析
  ├── decode/fileio       调用方 io.ReaderAt 的格式路由、raw payload
  └── compat.go           旧符号转发

decode 之外：
cmd/keysight-i2c-decode    Keysight VISA/SCPI 实时读取与设置恢复
cmd/tektronix-i2c-decode   Tektronix VISA/SCPI（inst0::INSTR）实时读取
wavefile                   打开保存文件，交给 decode/fileio.Read 解析
```

- `decode/core` 不导入总线、厂商、SCPI 或 VISA。
- `bus/i2c`、`bus/smi` 只依赖 `core` 和标准库。
- `decode/scope/*` 只做响应 payload/文件格式解析；不导入 `context`，不发送 SCPI，
  不打开文件，不依赖总线包。
- `decode/fileio` 只在调用方提供的 `io.ReaderAt` 上做格式识别和路由。
- 实时 SCPI 不抽成公共包：Keysight 和 Tektronix 的完整流程分别写在两个 cmd
  中，直接调用 `go-visa` 的 `Query`/`WriteString`/`QueryBinaryBlock`。
- `wavefile` 在 `decode/` 之外打开保存文件；decode 只接收已打开的数据流。
- 根包只导入 `core`、`bus/*` 和兼容转发所需的 `scope/*`；不导入 `fileio`、
  `wavefile` 或 `go-visa`。
- `go list -deps ./...` 无 import cycle。

### 2.1 职责边界

`decode` 的输入只能是已经取得的 `Waveform`、响应 payload、`io.ReaderAt` 或
`[]byte`：

- 不接收 `*visa.Session`，不接收任何 `SCPITransport`，不出现 `context` 超时；
- 不发送 SCPI 命令，不发送 `:STOP`，不查询 `*IDN?`/preamble/points/data；
- 不调用 `os.Open`，不接收文件路径；打开保存文件由 `wavefile` 完成；
- 文件格式解析只读取调用方已经打开的 reader。

实时读取全部写在 `cmd/keysight-i2c-decode` 和 `cmd/tektronix-i2c-decode`，
直接使用 `go-visa`；保存文件打开位于 `wavefile`（本仓库之外）。cmd 负责把采集
结果与 `decode.I2C/SMI` 连接起来。

---

## 3. 对外 API

```go
func decode.I2C(scl, sda Waveform, opts I2COptions) (I2CResult, error)
func decode.SMI(mdc, mdio Waveform, opts SMIOptions) (SMIResult, error)
```

旧入口保留并标记 Deprecated：

```go
func decode.DecodeI2C(...) (I2CResult, error)
func decode.DecodeSMI(...) (SMIResult, error)
```

总线包可直接使用：

```go
i2c.Decode(scl, sda, i2c.Options{...}) (i2c.Result, error)
smi.Decode(mdc, mdio, smi.Options{...}) (smi.Result, error)
```

核心类型通过根包类型别名转出：`Waveform`、`Timing`、`TimingName`、
`TimingFilter`、`Direction`、`Diagnostic`、`LogicThreshold`、`LogicTrace`、
`Edge`、`SelectionMode`、`FrequencyMeasurement`；I²C/SMI 类型别名保持
`I2COptions`、`I2CResult`、`I2CFrame`、`I2CTransaction`、`I2CByte`、`ACKStatus`、
`SMIOptions`、`SMIResult`、`SMIFrame`、`ValidationStatus`。

结果仍为强类型结构体：`result.Frames`、`result.Timings`、`timing.X1/X2`、
`diagnostic.Code`；没有 JSON 字符串或 `map[string]any` 作为 API 结果。
`I2CResult.Frames` / `Timings` 携带 `TransactionIndex`，`SMIResult.Timings` 携带
`FrameIndex`。

---

## 4. 多来源接入

### 4.1 内存波形

`decode.I2C` / `decode.SMI` 直接接收 `core.Waveform`（或根包别名
`decode.Waveform`），算法与改造前一致。

### 4.2 Keysight 实时 SCPI

完整代码在 `cmd/keysight-i2c-decode/main.go`，直接对 `*visa.Session` 调用
`Query`/`WriteString`/`QueryBinaryBlock`：

- `*IDN?` 后区分 Infiniium / InfiniiVision；
- 可选 `:STOP` + `*OPC?`；
- 保存并恢复 `:WAVeform:SOURce/FORMat/BYTEorder/UNSigned`；
- `:WAVeform:FORMat WORD`、`:WAVeform:BYTEorder LSBFirst`；
- 每通道 `:WAVeform:SOURce`、`PREamble?`、`POINts?`、`DATA?`；
- 窗口按 1-based `:WAVeform:DATA? start,size` 修正 `XOrigin`；
- 解析和换算复用 `decode/scope/keysight` 的纯函数；
- 所有命令和响应都打印到 stderr（`SCPI TX` / `SCPI RX`）。

### 4.3 Tektronix 实时 SCPI

完整代码在 `cmd/tektronix-i2c-decode/main.go`，使用
`TCPIP0::<addr>::inst0::INSTR`，直接调用 go-visa。命令顺序依据
`4-5-6-Series-MSO_Programmer.pdf` “Transferring waveform data from the
instrument”：

- `*IDN?`；
- `ACQuire:STATE STOP` + `*OPC?`；
- `:DATa:SOUrce CH1/CH2`、`:DATa:ENCdg SRIbinary`、`:DATa:WIDth 2`；
- `:HORizontal:RECOrdlength?`、`:DATa:STARt 1`、`:DATa:STOP <record>`；
- `:WFMOutpre:NR_Pt?`、`:WFMOutpre?`、`:CURVe?`；
- 解析 `BYT_Nr/BIT_Nr/ENCdg/BN_Fmt/BYT_Or/NR_Pt/PT_Fmt/XINcr/XZERo/PT_Off/
  YMUlt/YOFf/YZERo`；
- `V=((raw-YOff)*YMult)+YZero`，`t=XZero+(i-PTOffset)*XIncr`；
- 恢复 `:DATa:SOUrce/ENCdg/WIDth/STARt/STOP`；
- 所有命令和二进制块长度都打印到 stderr。

### 4.4 Keysight 原生 `.bin`（AG10）

`scope/keysight.ReadBin`（纯解析，调用方提供 `io.ReaderAt`）：

- 文件头 `AG` + 版本 `10`，12 字节 File Header、140 字节 Waveform Header、
  12 字节 Waveform Data Header；
- 支持 normal float32 buffer（buffer type 1，4 bytes/point）、多 waveform/多 segment；
- Peak/Envelope、digital buffer、平均类型等特殊模式明确拒绝；
- 真实样例 `生产环境测试/i2c-keysight/scl.bin` / `sda.bin` 解析并解码 I²C：
  2 transactions、3 frames（0x70 write、0x30 write、0x30 read）、159 timings，
  与已有 `i2c_capture.log` 的字段一致。

### 4.5 Tektronix 原生 `.wfm`（WFM#002/#003）

`scope/tektronix.ReadWFM`（纯解析，调用方提供 `io.ReaderAt`）：

- 解析 byte-order/version、BYT_NR、curve offset、exp_dim1/imp_dim1、
  update spec/curve object 和 precharge/postcharge 有效区；
- 支持 LE/BE、int8/int16/int32/uint32/uint64/float32/float64；
- 时间轴 `imp_offset + (data_start/bpp)*imp_scale`，样本从有效区开始；
- FastFrame、Envelope/Peak、非 vector 明确 unsupported；
- 真实样例 `生产环境测试/i2c-tek/scl.wfm` / `sda.wfm` 解析并解码 I²C
  （1 transaction，地址 0x4D）；与 CSV 的采样值逐点一致。

### 4.6 文件路由与 raw payload

`wavefile.Open`（decode 之外）打开文件，`fileio.Read` 在调用方提供的
`io.ReaderAt` 上按文件头识别：

- Keysight AG10 `.bin` → `scope/keysight.ReadBin`
- Tektronix `:WFM#002/#003` → `scope/tektronix.ReadWFM`
- Keysight 原生 `.wfm` → 明确 `ErrUnknownFormat`（不冒充 AG10）
- 其他/无头文件 → `ErrUnknownFormat`

`fileio.ReadRaw(payload, RawMetadata{Preamble, Encoding, Channel})` 只在显式提供
缩放与编码元数据时还原 raw SCPI payload；`fileio.Read` 对无 Preamble 的 raw `.bin`
拒绝，不猜测 SampleRate/StartTime。

`cmd/decode-file` 已支持两个文件/同一文件两通道、I²C/SMI、通道/segment 选择、
点数/文件大小限制和可选 JSON 输出。

`cmd/i2c-file-decode` 是面向 I²C 的专用文件示例：`-scl`/`-sda` 分别传入文件，
`wavefile.Open` 打开文件，`fileio.Read` 自动按文件头识别 Keysight AG10 `.bin` 或 Tektronix
WFM#002/#003 `.wfm`，打印事务/帧/字节和全部 timing（Value、X1、X2、
TransactionIndex、FrameIndex、ByteIndex、BitIndex、Phase、Direction），并用
`internal/waveplot` 生成 PNG。通道标签不匹配时，对单通道文件回退到该 segment
的唯一记录。

---

## 5. 验证结果

```bash
go test ./...
go vet ./...
```

- 全部包通过：`decode`、`core`、`bus/i2c`、`bus/smi`、`scope/keysight`、
  `scope/tektronix`、`fileio`、`wavefile`、`internal/waveplot`；共 129 个测试
  （含子测试）。
- 真实样例测试：Keysight AG10 `.bin`、Tektronix `WFM#003` `.wfm` 均通过；
  若样本目录不存在则测试 `t.Skip`（不伪造通过）。
- `cmd/i2c-file-decode` 用真实样例端到端运行通过：
  - Keysight `.bin`：2 transactions、3 frames（0x70 write 0x08、0x30 write 0x03、
    0x30 read 0x15 NACK）、159 timings；
  - Tektronix `.wfm`：1 transaction、1 frame（0x4D write，数据 0x00 0x05）、
    81 timings；
  - PNG：`decode/docs/images/keysight_bin_i2c.png`、
    `decode/docs/images/tektronix_wfm_i2c.png`。
- `go vet ./...` 无输出。
- `go test -race ./...` 因当前环境没有 gcc/clang、cgo 不可用而无法执行；不是测试失败。
- `go list -deps ./...` 无 import cycle。
- 没有为了通过测试删除诊断、放宽断言或替换算法。

---

## 6. 支持矩阵

| 能力/格式 | 状态 | 备注 |
| --- | --- | --- |
| I²C / SMI 算法与时序 | 已实现并离线验证 | Golden Vectors + 合成波形 |
| Keysight 实时 SCPI | 已实现（cmd 直接调用 VISA） | 波形换算此前 MXR604A 实机验证；新版命令通过编译 |
| Tektronix 实时 SCPI | 已实现（cmd 直接调用 VISA） | 按 4/5/6-Series 手册实现；编译通过，无实机 |
| Keysight `.bin` AG10 normal float32 | 已实现并真实样例离线验证 | 真实 scl/sda.bin 解码 I²C |
| Tektronix `.wfm` WFM#002/#003 normal YT | 已实现并真实样例离线验证 | 真实 scl/sda.wfm 解码 I²C |
| Keysight `.wfm` | 明确不支持 | 文件头不是 AG10，不猜测 |
| Tektronix CSV | 未实现 | 本轮不声明支持 |
| Raw SCPI payload | 显式 Preamble+Encoding 后支持 | 无元数据明确拒绝 |
| FastFrame / Envelope / Peak / 数字 buffer | 明确 unsupported | 不伪装为普通等间隔模拟波形 |
| 10-bit I²C、SMBus/PMBus、Clause 45、SPI/UART/CAN | 不支持 | 首期范围外 |

---

## 7. 未完成 / 待实机验证

1. **Tektronix 实时实机**：当前无泰克仪器；`cmd/tektronix-i2c-decode` 已按
   4/5/6-Series 手册实现并通过编译，但未验证 `*IDN?` 系列、`DATa:ENCdg` 支持、
   实际采样点数和 `WFMOutpre` 字段组合。真实 `.wfm` 文件解析可通过，但实时路径
   不能宣称已实机通过。
2. **Keysight 真实 I²C/MDIO 总线**：MXR604A 的波形换算/`scope/keysight` 读取
   路径已有实机只读验证记录，但没有可控 I²C/MDIO 信号源，未做真实协议验收；
   `:MARKer:` 光标写入/回读仍未验证。
3. **Keysight `.bin` / Tek `.wfm` 覆盖范围**：只实现 AG10 normal float32 与
   WFM#002/#003 normal YT；Peak/Envelope、FastFrame、数字通道、Keysight `.wfm`、
   Tek CSV 均明确不支持。没有真实多 segment / 多通道文件样本时，多记录路径仅有
   结构测试（当前真实 Keysight 文件为单 waveform；多 waveform 逻辑未用真实样本验证）。
4. **大记录**：80M 点完整记录转换为 `[]float64` 约 640MB/通道，当前通过
   `MaxPoints`/窗口/响应上限拒绝或限制；没有实现流式块解码。
5. **race**：当前环境缺 C 编译器，未执行；需在具备 cgo 工具链的机器补跑。
6. **文件时间轴**：Tektronix WFM 使用官方 `imp_dim` + precharge 偏移计算；
   与 CSV 的值逐点一致，但 CSV 时间轴带分数 zero-index，二者存在亚采样量级差异，
   未用硬件触发参考独立确认。Keysight `.bin` 使用 header 的 `XOrigin`/`XIncrement`。
7. **原始 SCPI 保存**：`cmd/keysight-download` 只保存剥离块头的 payload，必须配套
   元数据才能恢复；本轮未实现自动 `.meta.json` 持久化。

---

## 8. 主要文件

```text
decode/
├── api.go / compat.go / decode.go
├── core/{waveform,threshold,edge,timing,diagnostics,capture,errors,helpers}.go
├── bus/i2c/{decoder,timing}.go
├── bus/smi/{decoder,timing}.go
├── scope/binaryblock.go
├── scope/keysight/{preamble,bin}.go
├── scope/tektronix/{preamble,wfm}.go
├── fileio/open.go
├── testdata/
├── examples/
└── docs/

wavefile/open.go               # 打开文件后交给 decode/fileio.Read

cmd/
├── keysight-waveform-check/   # 实机只读校验（新 API 兼容）
├── keysight-i2c-decode/       # VISA/SCPI → decode.I2C → PNG（完整代码）
├── tektronix-i2c-decode/      # inst0::INSTR → SCPI → decode.I2C → PNG（完整代码）
├── decode-file/               # wavefile → decode.I2C/SMI
├── i2c-file-decode/           # SCL/SDA 文件 → I2C 时序 + PNG（真实样例）
└── keysight-download/         # 旧的一次性 raw payload 下载脚本（已移出根目录）
```

---

## 9. 兼容性与迁移说明

- 旧 `decode.DecodeI2C/DecodeSMI` 保留为 Deprecated 转发，现有调用可编译。
- 旧 `decode.FormatKeysightWaveform`、`ParseKeysightPreamble`、
  `ParseIEEE4882DefiniteBlock` 保留根包转发；新代码可直接使用
  `scope/keysight` 与 `scope`。
- Tektronix 不再返回 `ErrNotImplemented`；`decode.TektronixPreamble` 现在是
  WFMOutpre 字段集合，旧空结构体调用需按新字段迁移。
- `decode.I2CResult.Frames/Timings`、`decode.SMIResult.Frames/Timings` 保持强类型；
  `SMIResult.Timings` 是跨帧展开视图，`Timing.FrameIndex` 指回 frame。
- 根包仍不导入 `fileio` 或 `go-visa`；操作仪器只在 `cmd/`。
