# GOT Features

เอกสารนี้สรุปความสามารถที่ใช้งานได้จริงใน GOT CLI ปัจจุบัน

GOT เป็น CLI และ source generator สำหรับสร้าง Go/Fiber application โค้ดที่
สร้างขึ้นเป็นของ application และไม่ต้องพึ่ง GOT runtime ขณะทำงาน โปรเจกต์ใหม่
ใช้ Go 1.25, Fiber, Viper และ GORM

## คำสั่งที่ใช้งานได้

### สร้างโปรเจกต์

```sh
got init
got init service
got init workspace
```

ตัวอย่าง standard API:

```sh
got init service \
  --name catalog-api \
  --module example.com/catalog-api \
  --architecture standard \
  --db pg
```

ตัวอย่าง microservice:

```sh
got init service \
  --name ordering \
  --module example.com/ordering \
  --architecture microservice \
  --db pg
```

ตัวอย่าง workspace ที่มีหลาย microservices:

```sh
got init workspace \
  --name platform \
  --module example.com/platform \
  --services order,auth,payment \
  --db pg
```

Project generation รองรับ PostgreSQL, MySQL และ SQL Server

### สร้าง API feature

```sh
got api --name users
```

คำสั่งสร้าง entity, schema, repository, service, handler และ route พร้อม
register route ให้อัตโนมัติ ส่วน business logic เป็น scaffold ที่ผู้พัฒนาต้อง
implement

### สร้าง CRUD จากฐานข้อมูล

```sh
got generate crud
```

GOT อ่าน table, column, data type และ primary key แล้วสร้าง entity,
request schema, repository, service, handler และ routes สำหรับ List, Get,
Create, Update และ Delete จากนั้นรัน `go mod tidy` ให้อัตโนมัติ

| Endpoint | พฤติกรรม |
| --- | --- |
| `GET /api/<table>?limit=20&offset=0` | แบ่งหน้า เรียงตาม primary key คืน `items`, `total`, `limit`, `offset` (limit สูงสุด 100) |
| `GET /api/<table>/:id` | `404` เมื่อไม่พบ |
| `POST /api/<table>` | `201` ตรวจ request แล้วคืน `422` พร้อม error ราย field |
| `PUT` / `PATCH /api/<table>/:id` | partial update: field ที่ไม่ส่งจะคงค่าเดิม, `404` เมื่อไม่พบ |
| `DELETE /api/<table>/:id` | `204` เมื่อสำเร็จ, `404` เมื่อไม่พบ |

Request schema (`schemas/<table>`) แยกจาก entity:

- ไม่รับ primary key ที่ database สร้างเอง และไม่รับ `created_at`, `updated_at`, `deleted_at`
- Column ที่เป็น `NOT NULL` และไม่มี default ถือเป็น required; string ที่ required ต้องไม่ว่าง
- PostgreSQL enum ตรวจว่าค่าอยู่ในรายการที่อนุญาต
- ปฏิเสธ JSON field ที่ไม่รู้จัก (`400`) เพื่อจับการพิมพ์ชื่อผิด
- Column ที่มี database default เป็น pointer พร้อม `default:(-)` จึงใช้ค่า
  default เมื่อไม่ส่งมา แต่ยังบันทึกค่าที่ส่งมาตรงๆ เช่น `false` ได้

ใช้ `--tables` เลือกเฉพาะบางตาราง เช่น ตารางที่เพิ่มทีหลัง และ `--dry-run`
เพื่อดูรายการไฟล์ก่อนสร้างจริง

Schema introspection รองรับ PostgreSQL และ SQL Server ส่วน MySQL ใช้สร้าง
project ได้ แต่ยังไม่รองรับ schema introspection

### เพิ่ม method เข้า feature

```sh
got add method \
  --folder orders \
  --name Approve \
  --http-method PATCH \
  --path /:id/approve
```

คำสั่งแก้ repository, service, handler และ route ที่เกี่ยวข้องโดยใช้ Go AST
เมื่อแก้ source เดิม

### เพิ่ม JWT authentication

```sh
got add auth --jwt
```

สิ่งที่สร้าง:

- Access และ refresh tokens
- Bearer middleware
- Roles ใน JWT claims
- Issuer, audience, algorithm, expiry และ token-type validation
- Configuration สำหรับ access/refresh secrets ผ่าน environment variables

Login, password hashing, user lookup, token rotation และ revocation เป็น
application หรือ optional-module concern และยังไม่ได้สร้างให้

### เพิ่ม file upload

```sh
got add upload
```

สร้าง `POST /api/uploads` พร้อม local storage, storage interface, random stored
filename, path-traversal protection, file-size limit, extension validation และ
content-type detection สามารถเปลี่ยน storage implementation เป็น S3 หรือ MinIO
ได้ภายหลัง

### เพิ่ม Swagger/OpenAPI

```sh
got add swagger
```

สร้าง endpoint:

```text
/swagger/index.html
/swagger/openapi.json
```

ระบบค้นหา Fiber routes เพื่อเติม route ลง OpenAPI document ตอน runtime และ
ปิด Swagger อัตโนมัติเมื่อ `APP_ENV=production`

### เพิ่ม service ใน workspace

```sh
got add service notification
```

คำสั่งสร้าง service ใหม่ใน `services/` เชื่อมกับ shared module `libs/` และอัปเดต
`go.work`, `deploy/compose.yml` และ workspace README โดยรักษา service และ port เดิมไว้

Workspace มีโครงสร้าง `services/` (แต่ละ service มี `go.mod` ของตัวเอง),
`libs/` (shared logger และ middleware), `deploy/compose.yml`, `go.work`,
Makefile และ `got-workspace.json` ทุก service อ้าง `libs` ผ่าน `replace`
directive จึง build ได้ทั้งแบบมีและไม่มี `go.work` ใช้ `--shared-module` และ
`--shared-version` เมื่อแยก shared module ไปไว้คนละ repository

### ตรวจ environment และ project

```sh
got doctor
got doctor --project
got doctor --project --connect
got doctor --project --json
```

Doctor ตรวจ GOT/Go version, Git, Docker, project structure, configuration,
database/JWT settings, writable directories, database endpoint, readiness
endpoint และทุก service ใน workspace ค่า secret จะไม่ถูกพิมพ์ออกมา

### ดู route และ config

```sh
got routes
got routes --json
got config
got config --json
```

`got routes` อ่าน source แล้วแสดง method, path, handler และตำแหน่งไฟล์ของทุก
route โดยไม่ต้อง build หรือรันแอป และจะ error เมื่อมี route ซ้ำ ซึ่ง Fiber
จะใช้แค่ตัวแรกโดยไม่แจ้งเตือน

`got config` แสดงค่า config ที่แอปจะใช้จริงหลังรวม environment variable แล้ว
พร้อมบอกว่าแต่ละค่ามาจาก `config.yml` หรือ env ตัวไหน ค่า secret จะไม่ถูกแสดง

ทั้งสองคำสั่งใช้ที่ workspace root ได้ โดยจะแสดงทุก service

### อัปเกรดโปรเจกต์เดิม

```sh
got upgrade --dry-run
got upgrade
```

นำ fix จาก GOT เวอร์ชันใหม่ไปใส่ในโปรเจกต์ที่สร้างไปแล้ว ทุก step ตรวจจาก
ไฟล์จริงจึงใช้กับโปรเจกต์เก่าที่ไม่มี `got-project.json` ได้ ถ้า dev แก้โค้ด
ส่วนนั้นไปแล้ว ระบบจะรายงานเป็น manual action โดยไม่แตะไฟล์ และจะไม่เขียนทับ
ไฟล์ที่มี Git changes ค้างอยู่ ยกเว้นใช้ `--force`

โปรเจกต์ใหม่มี `got-project.json` บันทึกเวอร์ชัน GOT, module, architecture และ
database

### Version และ shell completion

```sh
got version
got completion zsh
got completion bash
got completion fish
```

## ความสามารถของ generated application

ทุก architecture มี:

- Fiber HTTP server
- Viper configuration และ environment-variable overrides
- GORM database integration
- Route -> handler -> service -> repository layering
- Response envelope และ error handler กลาง
- Structured JSON logging
- Request ID และ context propagation
- Request, service และ database logs
- Secret redaction และ bounded result logging
- HTTP request body limit

Microservice architecture เพิ่ม:

- Graceful shutdown
- HTTP read, write และ idle timeouts
- `GET /health/live`
- `GET /health/ready` พร้อม database readiness check
- Multi-stage Dockerfile ที่ใช้ Go 1.25
- Distroless runtime image

## ความปลอดภัยของ generator

- ไม่ overwrite project หรือ generated file ที่มีอยู่
- Rollback directory ใหม่เมื่อ project generation ล้มเหลว
- รับ database password ผ่าน masked prompt
- ไม่บันทึก password ที่ใช้ inspect database ลง project
- Database schema inspection เป็น read-only
- ใช้ tagged blueprints ที่ pin ใน `go.mod`

## ความสามารถที่ยังไม่มี

ยังไม่มีคำสั่ง `got new`, `got run`, `got migrate`, `got remove` และ `got modules`

ยังไม่มี migration engine, request-validation convention สำหรับ `got api`, password hashing,
RBAC enforcement, background jobs, queue, scheduler, cache, metrics, distributed
tracing และ email/notification abstraction ความสามารถเหล่านี้ต้องพิจารณาแยก
เป็น GOT Core, Community module, Enterprise module หรือ application concern ก่อน
พัฒนา เพื่อรักษาความเรียบง่ายของ Go

ดูรายละเอียดการใช้งานที่ [User Guide](../User-Guide.md) และผลวิเคราะห์
architecture/roadmap ที่ [Architecture Audit](architecture-audit.md)
