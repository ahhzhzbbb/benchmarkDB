# Công cụ Benchmark Golang: Đánh giá khả năng thay thế Aerospike bằng Redis trong hệ thống viễn thông 5G

Bạn là một senior Go engineer có kinh nghiệm về distributed systems, Kubernetes, Redis Cluster và Aerospike. Hãy xây dựng một công cụ benchmark bằng Golang để đánh giá khả năng thay thế Aerospike bằng Redis trong một hệ thống viễn thông 5G.

Mục tiêu của công cụ không phải chỉ đo OPS, mà phải tạo ra một benchmark có tính tái lập, công bằng và đủ thông tin để đưa ra quyết định migration Aerospike → Redis.

---

## 1. BỐI CẢNH HỆ THỐNG HIỆN TẠI

Hệ thống hiện tại sử dụng Aerospike làm distributed NoSQL database.

**Aerospike cluster:**
* 3 node/pod: `db-0`, `db-1`, `db-2`

**Topology Kubernetes hiện tại:**
* **db-0:**
  * Pod IP: 172.16.129.128
  * Kubernetes node: hl19f-5gc-com121
  * CPU hiện tại khoảng 18m
  * Memory hiện tại khoảng 4838 MiB
  * Age: 73d
  * Restarts: 10
* **db-1:**
  * Pod IP: 172.16.6.162
  * Kubernetes node: hl19f-5gc-com122
  * CPU hiện tại khoảng 18m
  * Memory hiện tại khoảng 3630 MiB
  * Age: 27d
  * Restarts: 0
* **db-2:**
  * Pod IP: 172.16.225.178
  * Kubernetes node: hl19f-5gc-com120
  * CPU hiện tại khoảng 19m
  * Memory hiện tại khoảng 3062 MiB
  * Age: 23d
  * Restarts: 0

Aerospike sử dụng mô hình distributed/shared-nothing cluster và có replication factor = 2.

**Redis hiện tại đã được triển khai dưới dạng Redis Cluster trên Kubernetes.**

**Redis topology:**
* 6 Redis pods
* 3 masters
* 3 replicas
* 16384 hash slots được phân bổ đầy đủ
* Tất cả 6 node đều connected
* Redis Cluster phải được xem là MỘT cluster, không phải 6 Redis server độc lập.

**Redis cluster topology:**
* `redis-cluster-0`: master, slots 0–5460
* `redis-cluster-1`: master, slots 5461–10922
* `redis-cluster-2`: master, slots 10923–16383
* `redis-cluster-4`: replica của redis-cluster-0
* `redis-cluster-5`: replica của redis-cluster-1
* `redis-cluster-3`: replica của redis-cluster-2

Redis pods được phân bố trên 2 Kubernetes nodes.

**Lưu ý:** Benchmark client phải sử dụng Redis Cluster-aware client. Không được thiết kế benchmark theo kiểu gửi toàn bộ request tới một Redis pod/service duy nhất, vì như vậy sẽ không phản ánh đúng behavior của Redis Cluster.

---

## 2. AEROSPIKE WORKLOAD THỰC TẾ

Aerospike hiện có 4 namespace: `smf`, `sgwc`, `ctx`, `ipallocate`.
Trong đó Secondary Index chỉ được sử dụng trên `smf` và `sgwc`.
Tổng cộng hiện có 22 Secondary Indexes.

**Namespace `smf`:**
* **Set Session:**
  * FTEIDC - String
  * TEIDC - String
  * SEID - String
  * Dnn - String
  * IPType - String
  * IP - String
  * State - String
  * SNSSAI - String
  * UpfSelection - Numeric
  * Timer - String
  * Timer - Numeric
* **Set LawfulTask:**
  * X1ID - String
* **Set queueBypass:**
  * attachTime - Numeric

**Namespace `sgwc`:**
* **Set bearer:**
  * sgwcS11teid - Numeric
  * sgwcS5teid - Numeric
  * sgwc-seid - Numeric
  * insi - String
  * startTime - Numeric
* **Các set phụ:** `s5Index`, `s11Index`, `seidIndex`, `imsiToTeid`
  * Các set này đều có Secondary Index trên: `startTime` - Numeric

Hai namespace `ctx` và `ipallocate` hiện không có Secondary Index.

---

## 3. CÁC LOẠI QUERY HIỆN TẠI

Aerospike sử dụng Secondary Index để thực hiện query/filter.
Có hai loại query chính:

### A. Equality query
Dùng cho String và Numeric.
*Ví dụ:*
```sql
SELECT * FROM smf.Session WHERE IP = "10.0.0.1";
-- hoặc:
SELECT * FROM sgwc.bearer WHERE sgwcS11teid = 12345;
```
Ở application level, tương đương với: `Filter.Equal("bin_name", value)`

### B. Range query
Dùng cho Numeric fields.
*Ví dụ:*
```sql
SELECT * FROM sgwc.bearer WHERE startTime BETWEEN 1710000000 AND 1710003600;
```
Ở application level: `Filter.Range("startTime", minVal, maxVal)`

**Lưu ý:**
Không được giả định rằng mọi query đều là full-table scan.
Workload production chủ yếu sử dụng:
* Primary Key lookup
* Secondary Index equality query
* Secondary Index range query
* PUT
* DELETE
* TTL

Full Set Scan + Filter Expressions/UDF không phải workload chính và không cần đưa vào benchmark mặc định.

---

## 4. MỤC TIÊU BENCHMARK

Công cụ phải trả lời được:
1. Redis có thể thay thế Aerospike cho Primary-Key workload hay không?
2. Redis có thể thay thế Aerospike cho Secondary-Index workload hay không?
3. Redis có đáp ứng được equality query tương đương Aerospike Secondary Index hay không?
4. Redis có đáp ứng được range query tương đương Aerospike Secondary Index hay không?
5. Throughput và latency của Redis so với Aerospike như thế nào?
6. CPU và memory consumption của hai hệ thống như thế nào?
7. Với cùng resource allocation, database nào có performance tốt hơn?
8. Khi workload tăng, database nào scale tốt hơn?
9. Redis có tạo ra operational complexity hoặc resource overhead đáng kể hay không?

Không được chỉ đưa ra một con số OPS rồi kết luận database nào tốt hơn.

---

## 5. KIẾN TRÚC CÔNG CỤ

Viết tool bằng Golang.
Thiết kế theo architecture:

```text
       Benchmark Engine
              |
      +-------+-------+
      |               |
Redis Adapter    Aerospike Adapter
      |               |
Redis Cluster    Aerospike Cluster
```

Benchmark engine phải độc lập với database implementation. Không được viết logic benchmark trực tiếp phụ thuộc vào Redis SDK hoặc Aerospike SDK.

Thiết kế interface abstraction tương tự:

```go
type KVStore interface {
    Set(...)
    Get(...)
    Delete(...)
}

type SearchStore interface {
    EqualityQuery(...)
    RangeQuery(...)
}
```

* Redis adapter implement các interface này bằng Redis Cluster client.
* Aerospike adapter implement các interface này bằng Aerospike Go client.
* Database-specific code phải nằm trong adapter/package riêng.

---

## 6. REDIS BACKEND

Redis backend phải kết nối tới Redis Cluster. Sử dụng Redis Cluster-aware Go client.
Client phải:
* Discover cluster topology
* Route request theo Redis hash slot
* Xử lý MOVED/ASK nếu client library yêu cầu
* Hỗ trợ concurrent requests
* Không hard-code request vào một Redis pod duy nhất

Benchmark configuration phải cho phép truyền một hoặc nhiều startup nodes.
*Ví dụ:*
```yaml
redis:
  startup_nodes:
    - redis-cluster-0:6379
    - redis-cluster-1:6379
    - redis-cluster-2:6379
```
Không hard-code hostname/IP vào source code.

---

## 7. REDIS SEARCH / QUERY ENGINE

Secondary Index là một workload BẮT BUỘC. Không được bỏ qua phần này.
Redis phải được benchmark bằng Redis Query Engine / Redis Search nếu deployment hiện tại hỗ trợ capability này.

Trước khi implement Search benchmark, hãy kiểm tra Redis version/image/capability thực tế. Không được giả định rằng mọi Redis deployment đều có Redis Query Engine.
Nếu capability không tồn tại, tool phải báo lỗi rõ ràng:
*"Redis Query Engine/Search capability is required for Secondary Index benchmark but is not available."*

Không được âm thầm thay Search bằng SCAN + client-side filtering.
Đặc biệt: KHÔNG được implement một search engine riêng trong benchmark tool.

Benchmark tool chỉ đóng vai trò:
* Generate documents
* Create/configure index
* Execute query
* Collect result
* Measure latency/throughput

Redis chịu trách nhiệm indexing và query execution.

---

## 8. DATA MODEL

Cần xây dựng một data model có thể map giữa Aerospike và Redis.
*Ví dụ logical record:*
```json
{
  "id": "session:100001",
  "FTEIDC": "...",
  "TEIDC": "...",
  "SEID": "...",
  "Dnn": "...",
  "IPType": "...",
  "IP": "...",
  "State": "...",
  "SNSSAI": "...",
  "UpfSelection": 1,
  "Timer": 123456,
  "attachTime": 123456,
  "startTime": 123456
}
```

Không nhất thiết phải sử dụng toàn bộ fields trong mọi benchmark.
Quan trọng nhất là benchmark phải hỗ trợ:
* String equality
* Numeric equality
* Numeric range

Data generator phải tạo dữ liệu deterministic khi sử dụng cùng seed. Mục đích là để Aerospike và Redis nhận cùng một logical dataset.

---

## 9. WORKLOAD TYPES

Tool phải hỗ trợ ít nhất các workload sau.

**Workload 1: Primary-Key GET**
* Random hoặc deterministic key lookup.
* Đo: throughput, latency, error rate

**Workload 2: PUT / SET**
* Ghi record.
* Có thể cấu hình: key count, value size, TTL

**Workload 3: DELETE**
* Xóa record theo Primary Key.

**Workload 4: TTL**
* Ghi record với TTL do benchmark configuration quyết định.
* Aerospike: sử dụng record TTL
* Redis: sử dụng EXPIRE hoặc SET EX
* Không được dùng database-side default TTL để làm sai lệch benchmark.

**Workload 5: Secondary Index Equality Query**
* Aerospike: `Filter.Equal("IP", value)`
* Redis: tương đương query trên field IP bằng Redis Query Engine.
* Workload phải hỗ trợ cả String equality và Numeric equality.

**Workload 6: Secondary Index Range Query**
* Aerospike: `Filter.Range("startTime", min, max)`
* Redis: query tương đương trên numeric indexed field.
* Workload phải đo latency và throughput của query, không chỉ correctness.

---

## 10. MIXED WORKLOAD

Tool phải hỗ trợ mixed workload.
*Ví dụ cấu hình:*
```yaml
workload:
  type: mixed
  operations:
    get: 50
    set: 20
    delete: 10
    equality_query: 15
    range_query: 5
```

Các tỷ lệ phải configurable. Không hard-code workload ratio.
Nếu không có production traffic ratio, cung cấp một số benchmark profile mặc định:
* read-heavy
* balanced
* query-heavy

Nhưng phải ghi rõ đây là synthetic workload. Không được tự nhận các tỷ lệ synthetic này là workload production.

---

## 11. CONFIGURATION

Tool phải sử dụng configuration file hoặc CLI flags.
*Ví dụ benchmark.yaml:*
```yaml
database:
  type: redis
redis:
  startup_nodes:
    - redis-cluster-0:6379
aerospike:
  hosts:
    - db-0:3000
    - db-1:3000
    - db-2:3000
workload:
  type: mixed
  duration: 60s
  concurrency: 100
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
benchmark:
  warmup: 10s
  duration: 60s
```
Các field phải configurable. Không hard-code IP, hostname, namespace, set, concurrency, duration, workload ratio, hay dataset size.

---

## 12. CONCURRENCY MODEL

Benchmark engine phải hỗ trợ configurable concurrency.
*Ví dụ:* `--concurrency 1`, `--concurrency 10`, `--concurrency 50`, vv.

Có thể implement worker pool hoặc goroutine workers. Không tạo một goroutine mới vô hạn cho mỗi request.
Benchmark phải có:
* Warm-up phase
* Measurement phase

Không đưa warm-up vào kết quả chính.

---

## 13. LATENCY MEASUREMENT

Đo latency của từng operation. Ít nhất phải report:
* min
* average
* p50
* p95
* p99
* max

Latency phải được đo ở benchmark client.
Phải phân biệt request latency và throughput. Không được lấy average latency để suy ra percentile.
Latency histogram phải được thiết kế phù hợp để không lưu toàn bộ request latency vào memory nếu benchmark chạy hàng triệu request (có thể sử dụng HDR Histogram).

---

## 14. THROUGHPUT

Report:
* total requests
* successful requests
* failed requests
* requests/sec hoặc ops/sec
* elapsed time

Công thức: `throughput = successful_operations / measurement_duration`
Nếu có lỗi, phải report error rate. Không được coi request failed là successful operation.

---

## 15. RESOURCE METRICS

Benchmark tool phải có khả năng ghi metadata về môi trường benchmark.
Ít nhất:
* benchmark client pod
* Kubernetes node của benchmark client
* database type
* database topology
* concurrency
* dataset size
* value size
* workload
* duration

Nếu có thể, hỗ trợ thu thập CPU/memory của database pods từ Kubernetes Metrics API hoặc cho phép user cung cấp metrics bên ngoài.
Điều quan trọng là kết quả phải cho phép đối chiếu: Redis CPU/RAM vs Aerospike CPU/RAM.

---

## 16. FAIRNESS / EXPERIMENT DESIGN

Đây là yêu cầu quan trọng nhất. Aerospike và Redis phải được benchmark dưới cùng:
* dataset
* key distribution
* value size
* operation distribution
* concurrency
* duration
* client location/environment
* warm-up period
* measurement period

Benchmark client nên chạy dưới dạng Kubernetes Job. Cùng một benchmark client environment.
Benchmark phải ghi lại Kubernetes node mà benchmark Job đang chạy.

---

## 17. BENCHMARK PHASES

Tool nên hỗ trợ chạy benchmark theo các phase:
* **Phase 1: Connectivity / correctness:** Kiểm tra connect, SET/PUT, GET, DELETE, query equality, query range.
* **Phase 2: Single operation benchmark:** Chạy riêng từng operation.
* **Phase 3: Concurrency benchmark:** Tăng dần mức concurrency (1, 10, 50, 100...).
* **Phase 4: Mixed workload:** Chạy workload với operation ratio configurable.
* **Phase 5: Scaling / saturation:** Tăng concurrency để tìm throughput plateau, latency degradation, CPU saturation, error rate.

---

## 18. CORRECTNESS

Benchmark không được chỉ quan tâm performance.
Trước khi đo performance của query:
* Đảm bảo index tồn tại
* Đảm bảo dataset đã được load
* Đảm bảo query trả về expected result

Redis và Aerospike phải sử dụng cùng logical data (sử dụng deterministic dataset generator).
Benchmark phải detect: missing record, wrong value, wrong query result, timeout, connection error, serialization error.

---

## 19. OUTPUT

Tool phải hỗ trợ human-readable output.
*Ví dụ:*
```text
Database: Redis Cluster
Topology: 3 masters / 3 replicas
Benchmark client:
Kubernetes node: hl19f-5gc-comXXX
Workload:
GET: 50%
SET: 20%
DELETE: 10%
Equality Query: 15%
Range Query: 5%
Dataset:
Keys: 1,000,000
Value size: 1 KB
Concurrency: 100
Warmup: 10s
Duration: 60s
Results:
Operation       OPS    P50    P95    P99    Errors
GET             ...    ...    ...    ...    ...
SET             ...    ...    ...    ...    ...
DELETE          ...    ...    ...    ...    ...
EQUALITY_QUERY  ...    ...    ...    ...    ...
RANGE_QUERY     ...    ...    ...    ...    ...
Total throughput: ...
Error rate: ...
```

---

## 20. MACHINE-READABLE OUTPUT

Ngoài human-readable output, hỗ trợ JSON.
*Ví dụ:*
```json
{
  "database": "redis",
  "topology": {
    "masters": 3,
    "replicas": 3
  },
  "benchmark": {
    "concurrency": 100,
    "duration": "60s"
  },
  "dataset": {
    "key_count": 1000000,
    "value_size": 1024,
    "seed": 12345
  },
  "results": {
    "get": {
      "throughput": 0,
      "p50_ms": 0,
      "p95_ms": 0,
      "p99_ms": 0,
      "errors": 0
    }
  }
}
```
JSON output phải đủ thông tin để sau này có thể dùng Python/Grafana/Excel để phân tích.

---

## 21. KUBERNETES DEPLOYMENT

Benchmark tool phải có Dockerfile và Kubernetes Job manifest.

Benchmark Job phải có thể:
* Chạy một benchmark
* Ghi kết quả ra stdout
* Optionally ghi JSON result vào mounted volume
* Sau khi benchmark xong, Job có thể được xóa. (Không triển khai benchmark tool như một long-running Deployment nếu không cần thiết).

---

## 22. ERROR HANDLING

Tool phải xử lý rõ:
* Connection failure
* Timeout
* Context cancellation
* Redis MOVED/ASK nếu client chưa tự xử lý
* Aerospike query failure
* Unavailable Secondary Index
* Redis Query Engine unavailable
* Malformed configuration
* Invalid workload ratio
* Dataset generation error

Không swallow errors. Error output phải cho biết operation, database, error type, và error message.

---

## 23. CODE QUALITY

Code phải idiomatic Go. Ưu tiên:
* `context.Context`
* Structured configuration
* Interfaces cho database abstraction
* Package separation
* Dependency injection
* Deterministic test data
* Unit tests & Integration tests

Đề xuất structure:
```text
cmd/
  benchmark/
    main.go
internal/
  benchmark/
    engine.go
    worker.go
    metrics.go
    workload.go
  datastore/
    datastore.go
    redis/
      client.go
      kv.go
      search.go
    aerospike/
      client.go
      kv.go
      search.go
  dataset/
    generator.go
  config/
    config.go
  output/
    text.go
    json.go
deploy/
  Dockerfile
  benchmark-job.yaml
configs/
  example.yaml
tests/
  ...
```

---

## 24. IMPORTANT DESIGN CONSTRAINTS

**Không được:**
* Chỉ benchmark GET/SET rồi kết luận migration.
* Dùng SCAN + client-side filtering để giả lập Secondary Index trên Redis.
* Implement search engine riêng trong benchmark.
* Gửi toàn bộ Redis traffic tới một Redis pod.
* Benchmark Redis Cluster như 6 Redis instance độc lập.
* Dùng workload khác nhau cho Aerospike và Redis.
* Tự ý hard-code workload ratio mà không ghi rõ đó là synthetic assumption.
* Đưa warm-up requests vào kết quả performance.
* Chỉ report average latency.
* Bỏ qua error rate.
* Tự động thay đổi resource của database trong lúc benchmark.
* Đưa benchmark client vào cùng resource accounting với database mà không ghi nhận điều đó.

---

## 25. RESOURCE FAIRNESS

Benchmark phải hỗ trợ hai cách đánh giá:
* **Scenario A: Equal Resource:** Redis và Aerospike được cấp resource tương đương. (Mục tiêu: So sánh performance thuần túy dưới cùng resource budget).
* **Scenario B: Production/Optimized Resource:** Mỗi database được cấp resource phù hợp với deployment thực tế. (Mục tiêu: So sánh cost/performance và operational efficiency).

Không trộn hai scenario thành một kết quả. Kết quả phải ghi rõ scenario nào đang được benchmark.

---

## 26. SEARCH BENCHMARK MAPPING

Tạo abstraction cho logical query.
*Ví dụ:*
```go
// EqualityQuery: namespace, set, field, value
// RangeQuery: namespace, set, field, min, max
```
Aerospike adapter map thành: `Filter.Equal(...)`, `Filter.Range(...)`
Redis adapter map thành: Redis Query Engine query tương ứng.

Benchmark engine không được biết cú pháp query riêng của Aerospike hoặc Redis.

---

## 27. INDEX SETUP

Công cụ nên có phase/setup command riêng để chuẩn bị index.
*Ví dụ:* `benchmark setup-index` và `benchmark run`

Không tạo index lại trước mỗi request. Index creation time không được tính vào query latency. Phải đảm bảo index tồn tại và populate/ready trước khi chạy measurement phase.

---

## 28. DATASET GENERATION

Dataset generator phải deterministic (vd: `seed: 12345`). Cùng seed phải tạo ra logical dataset giống nhau cho cả hai hệ thống.
Support: configurable key count, value size, string cardinality, numeric ranges.
Đặc biệt cần kiểm soát selectivity của Secondary Index để query benchmark biết expected selectivity.

---

## 29. RESULT INTERPRETATION

Tool chỉ có nhiệm vụ đo và report. Không được tự động kết luận "Redis tốt hơn Aerospike" chỉ dựa trên một benchmark.
Output nên cung cấp đầy đủ data để người dùng phân tích. Nếu có phần summary, chỉ đưa ra quantitative comparison.

---

## 30. IMPLEMENTATION PROCESS

Thực hiện theo các bước:
1. Phân tích requirements và đưa ra proposed architecture.
2. Thiết kế package structure và interfaces.
3. Implement configuration + CLI.
4. Implement dataset generator.
5. Implement benchmark engine và metrics.
6. Implement Aerospike adapter.
7. Implement Redis Cluster adapter.
8. Implement Secondary Index / Redis Query Engine adapter.
9. Implement output JSON + human-readable.
10. Implement tests.
11. Implement Docker image.
12. Implement Kubernetes Job.
13. Provide example benchmark configurations.

---

## 31. FIRST TASK

Trước khi viết code, hãy:
* Tóm tắt requirements.
* Xác định các assumptions.
* Đưa ra architecture, package structure.
* Đề xuất Go libraries cần sử dụng.
* Chỉ ra những điểm có khả năng gây benchmark bias.
* Xác định những capability của Redis cần kiểm tra trước (đặc biệt Redis Query Engine/Search).
* Đưa ra implementation plan theo từng phase.

Nếu phát hiện requirement nào mâu thuẫn hoặc capability không tồn tại, KHÔNG tự ý workaround thay đổi semantics của benchmark. Hãy báo rõ vấn đề.
Mục tiêu: Tạo ra công cụ benchmark bằng Go trên K8s cung cấp cơ sở định lượng đáng tin cậy để đánh giá migration từ Aerospike sang Redis.