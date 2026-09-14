# Công cụ Benchmark Cơ sở dữ liệu: Aerospike vs Redis Cluster

Đây là công cụ benchmark được viết bằng Golang để đánh giá khả năng thay thế Aerospike bằng Redis Cluster cho hệ thống viễn thông 5G. Công cụ đo đạc throughput, latency percentiles, error rate cho các thao tác với Primary-Key và truy vấn trên Secondary-Index (sử dụng Redis Query Engine/RediSearch đối với Redis).

## 🚀 Tính năng chính

- **Kiến trúc adapter độc lập:** Hỗ trợ Aerospike và Redis Cluster.
- **Mô phỏng workload thực tế:** Hỗ trợ các workload đặc thù của 5G (GET, SET, DELETE, thao tác với TTL).
- **Secondary Index:** Benchmark các truy vấn Equality và Range Query sử dụng Aerospike Secondary Index và Redis Query Engine.
- **Mix Workload linh hoạt:** Khả năng định nghĩa các kịch bản workload tuỳ chỉnh (read-heavy, write-heavy, balanced).
- **Phân chia giai đoạn benchmark:** Gồm các phases: *connectivity*, *single*, *concurrency*, *mixed*, và *saturation*.
- **Native Kubernetes:** Thiết kế phù hợp triển khai bằng Kubernetes Job, ghi nhận trực tiếp kết quả để sử dụng với Prometheus/Grafana.

## 📦 Cài đặt & Build

Công cụ yêu cầu Go 1.20+.

```bash
# Build file thực thi local
go build -o benchmark cmd/benchmark/main.go

# Build Docker image cho Kubernetes
docker build -t db-benchmark:latest -f deploy/Dockerfile .
```

## 🛠 Cách sử dụng (CLI)

Công cụ bao gồm các sub-command chính sau:

```bash
# 1. Kiểm tra kết nối và tính năng (e.g. Redis Query Engine có hoạt động hay không)
./benchmark check --config configs/example.yaml

# 2. Tạo Secondary Indexes cho các tập dữ liệu
./benchmark setup-index --config configs/example.yaml

# 3. Nạp dữ liệu giả lập (Data seeding)
./benchmark load-data --config configs/example.yaml

# 4. Chạy toàn bộ Benchmark Pipeline
./benchmark run --config configs/example.yaml
```

*Ghi chú: Lệnh `run` sẽ tự động thực hiện cả 4 bước: Connect & Check -> Load Data -> Setup Index -> Run Phases.*

## ⚙️ Cấu hình (Configuration)

File cấu hình là xương sống của công cụ benchmark. Dưới đây là ví dụ cấu hình rút gọn:

```yaml
# database: redis hoặc aerospike
database:
  type: redis

redis:
  startup_nodes:
    - redis-cluster-0:6379
    - redis-cluster-1:6379
    - redis-cluster-2:6379

workload:
  type: mixed
  operations:
    get: 50
    set: 20
    delete: 10
    equality_query: 15
    range_query: 5

dataset:
  key_count: 1000000
  value_size: 1024
  seed: 12345
  namespace: benchmark
  set: session

benchmark:
  warmup: "10s"
  duration: "60s"
  concurrency: 100
  phases:
    - connectivity
    - single
    - mixed
    - saturation

output:
  format: both
  file: "/tmp/benchmark-result.json"
```

Thư mục `configs/` đã cung cấp sẵn một số profiles ví dụ:
- `example.yaml`: Tổng hợp tất cả tuỳ chọn cấu hình
- `profile-balanced.yaml`: Phân bổ đều giữa Read, Write và Query
- `profile-read-heavy.yaml`: Nhấn mạnh vào lookup qua GET và Query
- `profile-query-heavy.yaml`: Tập trung nhiều vào Secondary Index Query

## ☸️ Triển khai trên Kubernetes

Để chạy benchmark chính thức dưới cùng điều kiện resource, hãy triển khai bằng Kubernetes Job:

1. Chỉnh sửa file `deploy/benchmark-job.yaml` để cập nhật địa chỉ database đích và config mong muốn.
2. Áp dụng Job:
```bash
kubectl apply -f deploy/benchmark-job.yaml
```
3. Xem log tiến trình:
```bash
kubectl logs -f job/db-benchmark
```

## ⚠️ Lưu ý Quan trọng

- Công cụ **bắt buộc** Redis phải được bật Redis Query Engine (RediSearch) để thực thi bài test về Secondary Index.
- Output sinh ra dạng `json` hỗ trợ phân tích định lượng sau chạy test (so sánh Resource, Throughput và Latency).
- Dữ liệu chạy load test là dữ liệu synthetic (giả lập). Tỷ lệ mix-workload có thể điều chỉnh tại YAML sao cho phản ánh chính xác nhất traffic hệ thống Production của bạn.
