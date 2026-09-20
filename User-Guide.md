# คู่มือการใช้งาน GOT CLI

Go Templatify — General-purpose Go/Fiber Project Generator

ปรับปรุงล่าสุด: 17 กันยายน 2026

> GOT ไม่ได้ผูกกับระบบร้านอาหารหรือธุรกิจใดธุรกิจหนึ่ง สามารถใช้สร้าง API สำหรับระบบธนาคาร โรงพยาบาล การชำระเงิน ระบบภายในองค์กร ภาครัฐ หรือระบบทั่วไปได้ ชื่อในตัวอย่างเป็นเพียงตัวอย่างเท่านั้น

## 1. ความสามารถ

- สร้าง route, handler, service, repository, schema และ entity
- ใช้ response กลางที่มี `status`, `message` และ `data`
- ตั้งค่า PostgreSQL, MySQL หรือ SQL Server ผ่าน GORM
- อ่าน schema และสร้าง CRUD สำหรับ PostgreSQL และ SQL Server
- Structured JSON log สำหรับ HTTP, service และ database
- เพิ่ม JWT access/refresh tokens และ bearer middleware
- เพิ่ม multipart file upload พร้อม validation
- รองรับ health check, graceful shutdown, Docker และ environment variables
- สร้างได้ทั้ง single service และ multi-service workspace

## 2. การติดตั้ง

การติดตั้ง GOT จาก source ต้องใช้ Go 1.25 ขึ้นไป:

```sh
go install github.com/gotcli/got@latest
got version
```

หากคำสั่ง `got` ยังไม่อยู่ใน `PATH` ให้เพิ่ม Go binary directory ที่แสดงโดย
`go env GOPATH` (โดยทั่วไปคือ `$GOPATH/bin`) ลงใน `PATH`

สามารถ build จาก working tree เพื่อพัฒนาได้เช่นกัน:

```sh
go build -o bin/got .
./bin/got version
```

หาก repository เผยแพร่ release binaries ในอนาคต ให้ใช้เฉพาะ artifacts จาก
หน้า Releases ของ repository และตรวจ checksum ที่เผยแพร่พร้อม release นั้น

## 3. เลือกรูปแบบโครงการ

| ความต้องการ | คำสั่ง | ผลลัพธ์ |
| --- | --- | --- |
| API หนึ่งระบบหรือ modular monolith | `got init service --architecture standard` | หนึ่ง Go module พร้อม layered API |
| Service เดียวที่ deploy แยก | `got init service --architecture microservice` | เพิ่ม health check, graceful shutdown, timeout และ Docker |
| ระบบที่มีหลาย service | `got init workspace` | หลาย Go modules ภายใต้ root workspace |

เริ่มด้วย `standard` หากยังไม่ต้อง deploy และ scale แยก service เลือก `workspace` เมื่อขอบเขต service, ownership และ deployment lifecycle แยกกันชัดเจนแล้ว

## 4. สร้าง Single Service

Interactive mode:

```sh
got init services
```

CLI จะถาม architecture, project name, module และ database ตามลำดับ `got init` ยังใช้เป็นคำสั่งย่อของ `got init service` ได้

ระบุค่าทั้งหมดด้วย flags:

```sh
got init service \
  --name customer-api \
  --module example.com/customer-api \
  --architecture standard \
  --db pg

cd customer-api
go test ./...
go vet ./...
go run .
```

กติกาการตั้งชื่อ:

- Project name ใช้ lowercase, ตัวเลข, hyphen หรือ underscore
- Module ใช้ Go import path เช่น `company.example/platform/customer-api`
- API feature ใช้ lowercase snake_case เช่น `order_items`
- Method ใช้ exported Go identifier เช่น `Approve` หรือ `PublishOrder`

## 5. สร้าง CRUD จากฐานข้อมูล

สร้าง project ก่อน แล้วรัน `got generate crud` ภายใน project เพื่ออ่าน metadata แบบ read-only และสร้าง entity, repository, service, handler และ route ที่ทำงานได้

### PostgreSQL

```sh
got init service \
  --name ordering-api \
  --module example.com/ordering-api \
  --architecture microservice \
  --db pg

cd ordering-api
got generate crud
```

คำสั่งอ่าน host, port, database และ username จาก `config.yml` หากต้องการ override เฉพาะรอบที่ inspect:

```sh
got generate crud --db-host localhost --db-schema public
```

ค่า schema เริ่มต้นคือ `public`

### SQL Server

```sh
got init service \
  --name billing-api \
  --module example.com/billing-api \
  --architecture microservice \
  --db mssql

cd billing-api
got generate crud --db-host sql.internal --db-schema dbo
```

ค่า schema เริ่มต้นคือ `dbo`

Password รับผ่าน masked prompt เท่านั้น ไม่มี password flag จึงไม่เข้า shell history และไม่ถูกเขียนลง project หลังสร้างเสร็จให้กำหนด runtime password ผ่าน environment variable ตัวอย่าง module `example.com/ordering-api`:

```sh
export ORDERING_API_DB_PASSWORD="your-runtime-password"
go run .
```

ข้อกำหนด:

- Schema CRUD รองรับ PostgreSQL และ SQL Server
- แต่ละ table ต้องมี integer primary key หนึ่งคอลัมน์
- MySQL สร้าง project ได้ แต่ยังไม่รองรับ `got generate crud`
- Generator ไม่สร้าง table หรือ migration ให้อัตโนมัติ
- `got init service --generate-crud` ยังใช้ได้เพื่อ backward compatibility

## 6. สร้าง Microservice Workspace

```sh
got init workspace \
  --name business-platform \
  --module example.com/business-platform \
  --services account,payment,notification \
  --db pg
```

โครงสร้างที่ได้:

```text
business-platform/
├── account-service/
├── payment-service/
├── notification-service/
├── go.work
├── got-workspace.json
├── compose.yml
├── Makefile
└── README.md
```

แต่ละ service มี `go.mod`, config, health checks และ Dockerfile ของตัวเอง ไม่มีการแชร์ entity หรือ repository อัตโนมัติ

เพิ่ม service ภายหลังจาก workspace root:

```sh
cd business-platform
got add service reporting
```

ระบบจะสร้าง `reporting-service` และปรับ `go.work`, `compose.yml`, `Makefile` และ workspace README

ตรวจทุก service:

```sh
make test
make vet
```

## 7. เพิ่ม API และ Method

รันภายใน generated service:

```sh
got api --name users
got api --name order_items
```

คำสั่งสร้าง entity, schema, repository, service, handler และ route โดยไม่ overwrite ไฟล์เดิม Repository เป็น scaffold ที่ต้อง implement ตาม business logic หากต้องการ CRUD ที่ทำงานได้ทันทีจากฐานข้อมูลให้ใช้ `got generate crud`

เพิ่ม method ใน feature:

```sh
got add method \
  --folder orders \
  --name Approve \
  --http-method PATCH \
  --path /:id/approve
```

หาก folder มีอยู่ ระบบจะแก้ repository, service, handler และ route ด้วย Go syntax tree หากไม่มีจะ scaffold สี่ชั้นนี้ก่อน

```sh
got add method --folder payments --name Capture
```

HTTP method เริ่มต้นคือ `POST` และ path เริ่มต้นเป็น kebab-case เช่น `PublishOrder` กลายเป็น `/publish-order` Repository method ใหม่มี `TODO` panic จนกว่า developer จะ implement

## 8. เพิ่ม JWT Authentication

```sh
got add auth --jwt
```

สิ่งที่เพิ่ม:

- JWT access และ refresh token manager
- Fiber bearer middleware
- JWT config ใน `config.yml`
- `github.com/golang-jwt/jwt/v5`

กำหนด secrets ผ่าน environment variables:

```sh
export ORDERING_API_JWT_ACCESS_SECRET="secure-random-value-at-least-32-bytes"
export ORDERING_API_JWT_REFRESH_SECRET="different-secure-random-value-at-least-32-bytes"
```

Access/refresh secrets ต้องเป็นคนละค่าและยาวอย่างน้อย 32 bytes ส่วน login, password hashing และ user lookup เป็น business logic ที่ต้องพัฒนาเพิ่ม

## 9. เพิ่ม File Upload

```sh
got add upload
curl -F "file=@document.pdf" http://127.0.0.1:3000/api/uploads
```

Endpoint คือ `POST /api/uploads` และรับ multipart field ชื่อ `file` ระบบใช้ชื่อไฟล์แบบ random, ตัด path ฝั่ง client, ตรวจ extension และชนิดไฟล์ และใช้ storage interface ที่เปลี่ยนเป็น S3 หรือ MinIO ภายหลังได้

ค่าเริ่มต้นอนุญาต `.jpg`, `.jpeg`, `.png`, `.pdf` จำกัดไฟล์ 10 MiB และ HTTP body 12 MiB

## 10. เพิ่ม Swagger UI และ OpenAPI

รันภายใน generated service:

```sh
got add swagger
go run .
```

เปิดหน้า Swagger UI:

```text
http://127.0.0.1:3000/swagger/index.html
```

OpenAPI 3.0 specification:

```text
http://127.0.0.1:3000/swagger/openapi.json
```

ไฟล์ต้นฉบับอยู่ที่ `docs/openapi.json` และถูก embed เข้า compiled binary จึงไม่ต้อง copy spec แยกตอน deploy หน้า UI โหลด asset จาก jsDelivr ดังนั้น browser ต้องเชื่อมต่อ network ได้ แต่ endpoint JSON ทำงานได้โดยไม่ใช้ CDN

ค่า `SWAGGER_ENABLED` ควบคุมการเปิดใช้งาน และ Swagger จะปิดเสมอเมื่อ `APP_ENV=production` แม้ตั้ง `SWAGGER_ENABLED=true` Spec endpoint จะค้นหา Fiber routes และ HTTP methods ตอน runtime รวม route จาก `got api` และ `got add method` โดยแปลง path parameter เช่น `:id` เป็น OpenAPI parameter อัตโนมัติ Operation ที่ค้นพบจะใช้ response envelope กลาง ให้เพิ่ม request/response schema และคำอธิบายเฉพาะ business ใน `docs/openapi.json` ตาม API จริง

## 11. Configuration และ Environment Variables

ค่าเริ่มต้นอยู่ใน `config.yml` และ override ได้ด้วย environment variable ที่มี prefix จากชื่อสุดท้ายของ Go module

| Config | ค่าเริ่มต้น/ความหมาย |
| --- | --- |
| APP_PORT | 3000 |
| LOG_DIR | logs |
| HTTP_READ_TIMEOUT | 15s |
| HTTP_WRITE_TIMEOUT | 15s |
| HTTP_IDLE_TIMEOUT | 60s |
| HTTP_BODY_LIMIT | 12582912 bytes |
| SHUTDOWN_TIMEOUT | 10s |
| DB_HOST/PORT/NAME | ข้อมูลการเชื่อมต่อฐานข้อมูล |
| DB_USERNAME/PASSWORD | บัญชีฐานข้อมูล; ห้าม commit password |
| DB_SSL_MODE | disable; production ควรตั้งตามนโยบายองค์กร |
| DB_SLOW_QUERY_THRESHOLD | 200ms |
| SWAGGER_ENABLED | true เมื่อเพิ่ม Swagger; production จะปิด route เสมอ |

ตัวอย่าง module `example.com/customer-api`:

```sh
export CUSTOMER_API_APP_PORT=8080
export CUSTOMER_API_DB_HOST=db.internal
export CUSTOMER_API_DB_PASSWORD="your_password"
go run .
```

## 12. Response, Logging และ Health Check

Response กลาง:

```json
{
  "status": 200,
  "message": "OK",
  "data": {}
}
```

`status` ตรงกับ HTTP status จริง `message` ใช้ข้อความมาตรฐาน เช่น 200 OK, 201 Created, 400 Bad Request, 404 Not Found และ 500 Internal Server Error การลบสำเร็จตอบ 200 พร้อม `data: null`

Log เป็น newline-delimited JSON ที่ stdout และ `LOG_DIR/application.log`:

- `http.inbound`: method, path, client IP และ request ID
- `database`: duration, affected rows, slow-query state และ error
- `service`: service, operation, duration, error และ sanitized result_data
- `http.outbound`: status, path, request ID และ total duration

ระบบไม่ log request body, authorization header, credential, SQL text หรือ bound parameter ฟิลด์ password, secret, token, authorization และ cookie ถูก redact ผลลัพธ์เกิน 64 KiB ถูกแทนด้วย size summary

Microservice health endpoints:

```text
GET /health/live
GET /health/ready
```

`live` ตรวจ process ส่วน `ready` ตรวจความพร้อมรวม database connection

## 13. การทดสอบและรัน

Single service:

```sh
go test ./...
go test -race ./...
go vet ./...
go run .
```

Workspace:

```sh
make test
make vet
docker compose up --build
```

ก่อนรัน Docker ต้องตั้ง database host ที่ container เข้าถึงได้ `localhost` ภายใน container หมายถึง container ตัวเอง ไม่ใช่เครื่อง host

ตรวจ health:

```sh
curl http://127.0.0.1:3000/health/live
curl http://127.0.0.1:3000/health/ready
```

## 14. ตรวจความพร้อมด้วย GOT Doctor

ตรวจ toolchain และ environment ภายในเครื่องโดยไม่เชื่อมต่อระบบภายนอก:

```sh
got doctor
```

ตรวจ generated service หรือ workspace จาก root directory:

```sh
got doctor --project
```

รายการตรวจ project ประกอบด้วย `go.mod`, `config.yml`, database password, JWT secrets (ถ้ามี), log/upload directory และทุก service ใน workspace ค่า secret จะแสดงเพียงว่าตั้งค่าแล้วหรือไม่และไม่ถูกพิมพ์ออกมา

อนุญาตการตรวจภายนอกแบบ read-only:

```sh
got doctor --project --connect
```

`--connect` ตรวจ Go module DNS, database TCP endpoint และ local readiness endpoint โดยมี timeout สั้น การตรวจ endpoint เป็นการตรวจการเข้าถึง ไม่แก้ไขข้อมูลใน database

ใช้ใน CI หรือ automation:

```sh
got doctor --project --json
```

สถานะมี `PASS`, `WARN` และ `FAIL` โดย warning ไม่ทำให้คำสั่งล้มเหลว แต่หากมี fail อย่างน้อยหนึ่งรายการจะคืน non-zero exit code

## 15. Command Reference

| คำสั่ง | หน้าที่ |
| --- | --- |
| `got init service [flags]` | สร้าง standard API หรือ microservice หนึ่งตัว |
| `got init workspace [flags]` | สร้าง workspace หลาย microservices |
| `got init [flags]` | คำสั่งย่อเดิมของ init service |
| `got generate crud [flags]` | อ่าน database config ของ project และสร้าง CRUD |
| `got api --name <feature>` | สร้าง API feature ครบทุก layer |
| `got add method [flags]` | เพิ่ม method ในทุก feature layer |
| `got add auth --jwt` | เพิ่ม JWT access/refresh tokens |
| `got add upload` | เพิ่ม multipart upload endpoint |
| `got add swagger` | เพิ่ม OpenAPI 3.0 และ Swagger UI |
| `got add service <name>` | เพิ่ม service ใน workspace |
| `got doctor [flags]` | ตรวจ environment หรือ generated project |
| `got version` | แสดง version |
| `got completion <shell>` | สร้าง shell completion |

Service flags:

```text
-n, --name string          Project directory
-m, --module string        Go module import path
-a, --architecture string  standard หรือ microservice
-d, --db string            pg, mysql หรือ mssql
    --generate-crud        อ่าน database schema และสร้าง CRUD
    --db-host string       Database host
    --db-port uint16       Database port
    --db-name string       Database name
    --db-user string       Database username
    --db-schema string     Database schema
```

Workspace flags:

```text
-n, --name string       Workspace directory
-m, --module string     Module prefix ของ child services
    --services string   Service names คั่นด้วย comma
-d, --db string         pg, mysql หรือ mssql
```

Method flags:

```text
-f, --folder string        Feature folder
-n, --name string          Exported Go method
    --http-method string   GET, POST, PUT, PATCH หรือ DELETE
    --path string          Fiber route path
```

ดู help จาก binary ที่ติดตั้ง:

```sh
got --help
got init service --help
got init workspace --help
got generate crud --help
got api --help
got add method --help
got add auth --help
got add upload --help
got add swagger --help
got add service --help
got doctor --help
```

## 16. Troubleshooting

### `command not found: got`

ตรวจว่า binary directory อยู่ใน PATH แล้วเปิด terminal ใหม่:

```sh
which got
got version
```

### macOS เปิด Binary ไม่ได้

ตรวจ checksum และแหล่งที่มาก่อน แล้วรัน:

```sh
chmod +x "$HOME/.local/bin/got"
xattr -d com.apple.quarantine "$HOME/.local/bin/got"
```

### `relation "public.<table>" does not exist`

ตรวจว่า table มีจริงใน database/schema ที่ application เชื่อมต่อ และตรวจ `DB_HOST`, `DB_NAME`, `DB_SCHEMA` และ search path การ generate code ไม่ได้สร้าง table หรือ migration ให้อัตโนมัติ

### เชื่อม Database ไม่ได้

- ตรวจ host, port, database, username และ environment variable ของ password
- ตรวจ firewall, VPN, SSL mode และสิทธิ์ของ user
- หากรันใน Docker อย่าใช้ localhost เพื่ออ้าง database นอก container
- Schema inspection มี timeout 15 วินาที

### `destination already exists`

GOT ไม่ overwrite project directory ให้ใช้ชื่อใหม่หรือย้าย directory เดิมด้วยตนเองหลังตรวจข้อมูล

### API หรือ Service ซ้ำ

Generator ปฏิเสธชื่อเดิมเพื่อป้องกัน code สูญหาย `got add service` ต้องรันจาก workspace root ที่มี `got-workspace.json`

## 17. สถานะและ Roadmap

| ความสามารถ | สถานะ |
| --- | --- |
| PostgreSQL project + schema CRUD | Stable |
| MySQL project generation | Beta; ยังไม่มี schema CRUD |
| SQL Server project + schema CRUD | Beta |
| Oracle และ MongoDB | วางแผนเป็น optional packages |
| `got add redis` | เฟสถัดไป; ยังไม่มีใน release ปัจจุบัน |
| `got add kafka` | เฟสถัดไป; ยังไม่มีใน release ปัจจุบัน |

หาก binary เป็นคนละ version ให้ใช้ `got version` และ `got <command> --help` เป็นข้อมูลหลัก
