# Công cụ Benchmark Cơ sở dữ liệu: Aerospike vs Redis Cluster

Công cụ benchmark được xây dựng bằng **Golang** để đánh giá hiệu năng và khả năng thay thế **Aerospike** bằng **Redis Cluster** trong hệ thống viễn thông 5G. 

Công cụ giúp đo đạc định lượng khách quan các chỉ số throughput (OPS), latency percentiles (p50, p95, p99, min, max, avg), tỷ lệ lỗi (error rate) cho các thao tác **Primary-Key (CRUD)** và truy vấn **Secondary-Index (Equality Query & Range Query)**.

---

## 📋 Mục lục

1. [Giới thiệu & Bối cảnh](#-giới-thiệu--bối-cảnh)
2. [Yêu cầu tiền trạm (Prerequisites)](#-yêu-cầu-tiền-trạm-prerequisites)
3. [Cài đặt & Biên dịch](#-cài-đặt--biên-dịch)
4. [Cấu trúc Lệnh CLI (Command Reference)](#-cấu-trúc-lệnh-cli-command-reference)
5. [Cấu hình Chi tiết (Configuration Guide)](#-cấu-hình-chi-tiết-configuration-guide)
6. [Quy trình Chạy Benchmark & Các Giai đoạn (Phases)](#-quy-trình-chạy-benchmark--các-giai-đoạn-phases)
7. [Triển khai trên Kubernetes](#-triển-khai-trên-kubernetes)
8. [Đọc & Phân tích Kết quả (Results & Metrics)](#-đọc--phân-tích-kết-quả-results--metrics)
9. [Xử lý Lỗi Thường Gặp (Troubleshooting)](#-xử-lý-lỗi-thường-gặp-troubleshooting)

---

## 🎯 Giới thiệu & Bối cảnh

Trong hệ thống core 5G, cơ sở dữ liệu đóng vai trò lưu trữ trạng thái phiên (session), thông tin thuê bao, context điều hướng và cấp phát IP (`smf`, `sgwc`, `ctx`, `ipallocate`).

- **Aerospike:** Mô hình distributed NoSQL, hỗ trợ Primary Key CRUD và Secondary Index natively trên các Bin.
- **Redis Cluster:** Mô hình phân vùng Hash Slot (16,384 slots across Masters/Replicas). Để thực hiện truy vấn Secondary Index tương đương Aerospike, **Redis bắt buộc phải bật module Redis Query Engine (RediSearch / FT.SEARCH)**.

### 🌟 Tính năng cốt lõi của công cụ
- **Kiến trúc Ports & Adapters (Hexagonal Architecture):** Engine độc lập hoàn toàn với SDK của từng DB, đảm bảo tính công bằng và dễ mở rộng.
- **Tập dữ liệu Giả lập Khách quan (Deterministic Dataset):** Sinh dữ liệu bằng pseudo-random seed cố định, giúp Aerospike và Redis nhận đúng tập record và giá trị như nhau.
- **Hỗ trợ đầy đủ Workloads 5G:** Primary Key GET/SET/DELETE, TTL (Expirations), Secondary Index String/Numeric Equality Query và Numeric Range Query.
- **5 Giai đoạn Test (Phases):** Từ kiểm tra tính đúng đắn (`connectivity`), đo đơn lẻ (`single`), tăng dần tải (`concurrency`), hỗn hợp (`mixed`) đến điểm bão hòa (`saturation`).
- **Kịch bản Đánh giá Tài nguyên (Scenarios):** Hỗ trợ `equal_resource` (cùng CPU/RAM) và `production` (cấu hình tối ưu sản xuất).

---

## ⚡ Yêu cầu tiền trạm (Prerequisites)

- **Môi trường Local:**
  - **Go:** 1.20 trở lên
  - **Make:** Trình quản lý tác vụ (tùy chọn)
  - **Docker:** 20.10+ (để build container image)
- **Cơ sở dữ liệu Đích:**
  - **Aerospike Cluster:** Đã khởi tạo Namespace (ví dụ: `benchmark`) và cho phép tạo SIndex.
  - **Redis Cluster:** Đã kích hoạt **Redis Query Engine / RediSearch module** (`FT.CREATE`, `FT.SEARCH`). *Lưu ý: Nếu Redis không hỗ trợ RediSearch, công cụ sẽ từ chối chạy benchmark Secondary Index và báo lỗi trực tiếp.*
- **Môi trường K8s (cho thử nghiệm chính thức):**
  - Kubernetes Cluster 1.22+
  - Quyền tạo `ConfigMap` và `Job` trong target namespace.

---

## 🛠 Cài đặt & Biên dịch

### 1. Biên dịch Binary Local

```bash
# Sử dụng Go CLI
go build -ldflags="-w -s" -o benchmark cmd/benchmark/main.go

# Hoặc sử dụng Makefile
make build
```

### 2. Build Docker Image

```bash
# Build local image
docker build -t db-benchmark:latest -f deploy/Dockerfile .

# Hoặc qua Makefile
make docker-build
```

---

## 💻 Cấu trúc Lệnh CLI (Command Reference)

Cú pháp tổng quát:
```bash
./benchmark <command> [flags]
```

### Các Sub-commands chính

| Sub-command | Mô tả | Chi tiết thực thi |
| :--- | :--- | :--- |
| `check` | Kiểm tra kết nối & tính năng | Thực hiện Ping kết nối và verify capability của DB (kiểm tra RediSearch đối với Redis). |
| `setup-index` | Khởi tạo Secondary Index | Tạo trước các Secondary Index (Aerospike SIndex & RediSearch FT.CREATE) mà **không tính thời gian khởi tạo vào bài đo**. |
| `load-data` | Nạp dữ liệu thử nghiệm | Nạp tập dữ liệu giả lập (Data Seeding) dựa trên `key_count` và `seed` trong cấu hình. |
| `run` | Chạy toàn bộ pipeline | **Tự động thực hiện 4 bước liên tiếp:** `check` ➔ `load-data` ➔ `setup-index` ➔ Chạy các Benchmark Phases ➔ Export Report. |

### Ví dụ sử dụng CLI tiêu chuẩn

```bash
# 1. Kiểm tra tính sẵn sàng của Redis Cluster & RediSearch module
./benchmark check --config configs/example.yaml --database-type redis

# 2. Tạo trước Secondary Index trên database
./benchmark setup-index --config configs/example.yaml

# 3. Seeding dữ liệu giả lập (vd: 100,000 keys)
./benchmark load-data --config configs/example.yaml

# 4. Thực thi toàn bộ quá trình Benchmark
./benchmark run --config configs/example.yaml
```

### Các CLI Flags ghi đè (Flags Override)

Các flags dưới đây cho phép bạn ghi đè trực tiếp các tham số trong file cấu hình YAML:

- `--config`: Đường dẫn tới file cấu hình YAML (Ví dụ: `configs/example.yaml`).
- `--database-type`: Loại DB benchmark (`redis` hoặc `aerospike`).
- `--redis-startup-nodes`: Danh sách seed nodes của Redis Cluster (Ví dụ: `127.0.0.1:6379,127.0.0.1:6380`).
- `--redis-password`: Mật khẩu kết nối Redis.
- `--aerospike-hosts`: Danh sách seed hosts của Aerospike (Ví dụ: `127.0.0.1:3000`).
- `--aerospike-namespace`: Namespace cho Aerospike (Ví dụ: `benchmark`).
- `--workload-type`: Loại workload (`get`, `set`, `delete`, `ttl`, `equality_query`, `range_query`, `mixed`).
- `--dataset-key-count`: Số lượng record khởi tạo (Ví dụ: `100000`).
- `--dataset-value-size`: Dung lượng mỗi record theo bytes (Ví dụ: `1024`).
- `--dataset-seed`: Random seed cố định cho dữ liệu (Ví dụ: `12345`).
- `--benchmark-warmup`: Thời gian warm-up bỏ qua không tính kết quả (Ví dụ: `10s`).
- `--benchmark-duration`: Thời gian chạy đo đạc mỗi phase (Ví dụ: `60s`).
- `--benchmark-concurrency`: Số lượng worker đồng thời (Ví dụ: `100`).
- `--benchmark-scenario`: Kịch bản thử nghiệm (`equal_resource` hoặc `production`).
- `--output-format`: Định dạng báo cáo (`text`, `json`, `both`).
- `--output-file`: Đường dẫn lưu file kết quả JSON (Ví dụ: `/tmp/result.json`).

---

## ⚙️ Cấu hình Chi tiết (Configuration Guide)

File cấu hình YAML kiểm soát toàn bộ thông số hoạt động của bài kiểm thử.

### Cấu trúc File Cấu hình Mẫu (`configs/example.yaml`)

```yaml
# Loại database: "redis" hoặc "aerospike"
database:
  type: redis

# Thống số kết nối Redis Cluster
redis:
  startup_nodes:
    - 127.0.0.1:6379
    - 127.0.0.1:6380
    - 127.0.0.1:6381
  password: ""
  max_retries: 3
  pool_size: 100
  read_timeout: "3s"
  write_timeout: "3s"

# Thông số kết nối Aerospike Cluster
aerospike:
  hosts:
    - 127.0.0.1:3000
  namespace: benchmark
  auth_user: ""
  auth_password: ""
  total_timeout: "5s"
  socket_timeout: "3s"

# Định nghĩa Workload & Tỷ lệ các thao tác (đối với mixed)
workload:
  type: mixed # get, set, delete, ttl, equality_query, range_query, mixed
  operations:
    get: 50             # 50%
    set: 20             # 20%
    delete: 10          # 10%
    equality_query: 15  # 15%
    range_query: 5      # 5%

# Định nghĩa Tập dữ liệu sinh ra (Dataset)
dataset:
  key_count: 100000
  value_size: 1024      # Bytes
  seed: 12345           # Seed ngẫu nhiên cố định
  namespace: benchmark
  set: session
  string_cardinality: 1000
  numeric_min: 1700000000
  numeric_max: 1710000000
  ttl: "300s"

# Tham số điều khiển Benchmark
benchmark:
  warmup: "10s"
  duration: "60s"
  concurrency: 100
  phases:
    - connectivity
    - single
    - concurrency
    - mixed
    - saturation
  concurrency_steps: [1, 10, 50, 100, 200, 500]
  scenario: equal_resource # equal_resource hoặc production

# Cấu hình Đầu ra Kết quả
output:
  format: both          # text, json, both
  file: "/tmp/benchmark-result.json"
```

### Các Profiles Workload Cung Cấp Sẵn (`configs/`)

Thư mục `configs/` bao gồm 4 profiles chuẩn chuẩn bị sẵn cho các tình huống kiểm thử khác nhau:

1. `example.yaml`: Cấu hình tổng hợp đầy đủ tất cả các trường.
2. `profile-read-heavy.yaml`: Kịch bản tập trung đọc (**70% GET**, 15% SET, 5% DELETE, 7% Equality Query, 3% Range Query).
3. `profile-balanced.yaml`: Kịch bản cân bằng giữa CRUD và Query (**40% GET**, 25% SET, 10% DELETE, 15% Equality Query, 10% Range Query).
4. `profile-query-heavy.yaml`: Kịch bản tập trung truy vấn Secondary Index (**40% Equality Query**, **25% Range Query**, 20% GET, 10% SET, 5% DELETE).

---

## 🔄 Quy trình Chạy Benchmark & Các Giai đoạn (Phases)

Khi chạy lệnh `./benchmark run` (hoặc khai báo danh sách `phases` trong YAML), công cụ thực hiện lần lượt qua 5 giai đoạn:

```
┌─────────────────┐     ┌───────────────┐     ┌───────────────────┐     ┌───────────────┐     ┌──────────────────┐
│  1. Connectivity│ ──> │   2. Single   │ ──> │   3. Concurrency  │ ──> │   4. Mixed    │ ──> │  5. Saturation   │
│  (Sanity Check) │     │  Operations   │     │ (Scaling Worker)  │     │   Workload    │     │   (Stress Test)  │
└─────────────────┘     └───────────────┘     └───────────────────┘     └───────────────┘     └──────────────────┘
```

1. **Phase 1: Connectivity (Kiểm tra kết nối & Tính đúng đắn)**
   Thực hiện thử nghiệm mẫu 1 thao tác SET, GET, DELETE, Equality Query và Range Query để đảm bảo dữ liệu đọc/ghi đúng và các chỉ mục đã sẵn sàng trước khi đo tải nặng.
2. **Phase 2: Single Operation Benchmark (Đo đơn lẻ từng thao tác)**
   Chạy riêng biệt từng loại operation (`get`, `set`, `delete`, `equality_query`, `range_query`) trong khoảng thời gian `duration` để đo latency baseline chuẩn cho từng thao tác độc lập.
3. **Phase 3: Concurrency Scaling Benchmark (Đo mức độ mở rộng theo Worker)**
   Tăng dần số lượng goroutine/worker đồng thời theo các nấc khai báo trong `concurrency_steps` (ví dụ: `1`, `10`, `50`, `100`, `200`, `500`) để quan sát độ tăng trưởng throughput và độ trễ.
4. **Phase 4: Mixed Workload Benchmark (Đo tải hỗn hợp)**
   Thực thi tải hỗn hợp đồng thời theo tỷ lệ cấu hình trong `operations` đại diện cho lưu lượng thực tế.
5. **Phase 5: Saturation / Stress Test (Đo điểm bão hòa)**
   Đẩy mức concurrency lên cao tối đa nhằm tìm điểm bão hòa throughput (OPS plateau), thời điểm latency suy giảm mạnh (latency degradation) và tỷ lệ lỗi bùng phát.

---

## ☸️ Triển khai trên Kubernetes

Để loại bỏ sai số do mạng hông (network hop) local và đo đạc dưới điều kiện tài nguyên chuẩn hóa (cùng CPU/RAM request & limit), bài kiểm thử nên được thực hiện thông qua **Kubernetes Job**.

### 1. Cấu trúc File Manifest (`deploy/benchmark-job.yaml`)

File manifest bao gồm:
- **ConfigMap (`benchmark-config`):** Chứa file cấu hình `benchmark.yaml`.
- **Job (`db-benchmark`):** Chạy duy nhất 1 pod client thực thi lệnh `benchmark run --config /config/benchmark.yaml`.

### 2. Các bước triển khai chi tiết

```bash
# Bước 1: Build và Push Docker Image lên Registry của bạn
docker build -t your-registry.com/db-benchmark:v1.0.0 -f deploy/Dockerfile .
docker push your-registry.com/db-benchmark:v1.0.0

# Bước 2: Chỉnh sửa deploy/benchmark-job.yaml 
#  - Đổi tên image thành your-registry.com/db-benchmark:v1.0.0
#  - Cập nhật địa chỉ Redis Cluster / Aerospike trong ConfigMap

# Bước 3: Deploy Job lên Kubernetes
kubectl apply -f deploy/benchmark-job.yaml

# Bước 4: Theo dõi logs của bài benchmark trực tiếp
kubectl logs -f job/db-benchmark

# Bước 5: Sau khi xong, xóa Job để thu hồi tài nguyên
kubectl delete -f deploy/benchmark-job.yaml
```

### 3. Phím tắt thao tác nhanh với Makefile

```bash
make k8s-deploy  # Apply job vào K8s
make k8s-logs    # Stream log của job
make k8s-clean   # Xóa job khỏi K8s
```

---

## 📊 Đọc & Phân tích Kết quả (Results & Metrics)

Khi kết thúc bài test, công cụ xuất kết quả dưới dạng console human-readable và/hoặc file JSON machine-readable.

### 1. Ví dụ Báo cáo Console Output

```text
================================================================================
                          DATABASE BENCHMARK REPORT                             
================================================================================
Target Database : Redis Cluster
Scenario        : equal_resource
Client K8s Node : hl19f-5gc-com121
Timestamp       : 2026-09-14T17:20:00Z
--------------------------------------------------------------------------------
Dataset Summary:
  Keys          : 100,000
  Value Size    : 1,024 Bytes
  Seed          : 12345
--------------------------------------------------------------------------------
Phase: Mixed Workload (Concurrency: 100, Duration: 60s)

OPERATION       SUCCESS       FAIL      THROUGHPUT(OPS)   P50(ms)   P95(ms)   P99(ms)
GET             3,000,000     0         50,000.00         0.85      1.42      2.10
SET             1,200,000     0         20,000.00         1.10      1.95      3.05
DELETE          600,000       0         10,000.00         0.90      1.60      2.40
EQUALITY_QUERY  900,000       0         15,000.00         1.45      2.80      4.20
RANGE_QUERY     300,000       0         5,000.00          2.10      4.10      6.80
--------------------------------------------------------------------------------
TOTAL THROUGHPUT : 100,000.00 ops/sec
ERROR RATE       : 0.00 %
================================================================================
```

### 2. Cấu trúc File Kết quả JSON (`output.file`)

Bản ghi JSON chứa đầy đủ metadata về môi trường và chỉ số từng thao tác, thuận tiện cho việc vẽ biểu đồ so sánh qua Python/Grafana:

```json
{
  "database": "Redis Cluster",
  "scenario": "equal_resource",
  "metadata": {
    "kubernetes_node": "hl19f-5gc-com121",
    "concurrency": 100,
    "duration": "60s",
    "key_count": 100000,
    "value_size": 1024
  },
  "phases": [
    {
      "name": "mixed",
      "total_ops": 6000000,
      "elapsed_seconds": 60.0,
      "throughput_ops": 100000.0,
      "operations": {
        "get": {
          "count": 3000000,
          "errors": 0,
          "ops_per_sec": 50000.0,
          "latency_p50_ms": 0.85,
          "latency_p95_ms": 1.42,
          "latency_p99_ms": 2.10,
          "latency_min_ms": 0.21,
          "latency_max_ms": 12.4
        }
      }
    }
  ]
}
```

---

## ❓ Xử lý Lỗi Thường Gặp (Troubleshooting)

### 1. Lỗi: `Capability check failed: Redis Query Engine (RediSearch) module is not loaded`
- **Nguyên nhân:** Redis Cluster mục tiêu đang chạy phiên bản Redis Vanilla tiêu chuẩn mà chưa load module RediSearch (`redisearch.so`).
- **Giải pháp:** Chuyển sang sử dụng **Redis Stack Server** image (`redis/redis-stack-server:latest`) hoặc load kịch bản module RediSearch vào Redis nodes trước khi chạy benchmark.

### 2. Lỗi: `MOVED <slot> <ip:port>` hoặc `ASK` liên tục đối với Redis
- **Nguyên nhân:** Redis Cluster đang trong quá trình resharding slot hoặc client sử dụng IP pod không truy cập được từ bên ngoài.
- **Giải pháp:** Đảm bảo `redis.startup_nodes` khai báo đúng địa chỉ DNS/IP nội bộ của Pod trong K8s cluster và benchmark client pod chạy **bên trong cùng mạng K8s Cluster**.

### 3. Lỗi: `aerospike: namespace not found` hoặc `index not ready`
- **Nguyên nhân:** Aerospike namespace chưa được định nghĩa trong file cấu hình `/etc/aerospike/aerospike.conf` của DB node, hoặc SIndex đang trong quá trình xây dựng dở dang.
- **Giải pháp:** Chờ SIndex build hoàn tất (kiểm tra qua `aql` command `SHOW INDEXES`) trước khi thực thi benchmark.

---

> 💡 **Mẹo:** Để có kết quả đánh giá migration chính xác nhất, luôn chạy benchmark ở cùng một Node K8s Client và sử dụng kịch bản `equal_resource` với `dataset-seed` cố định cho cả Aerospike và Redis Cluster.

