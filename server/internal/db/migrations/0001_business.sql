-- 业务库初始结构（M0）
-- 约定：所有表带 created_at；状态字段用 text + 约束，避免枚举迁移麻烦
--
-- 回滚说明：本文件全部是 create ... if not exists，回滚只适用于「全新环境重建」：
--   drop table if exists tasks, hosts, clusters, azs, regions, refresh_tokens, users cascade;
--   drop extension if exists pgcrypto;
-- 已投产环境禁止直接执行上面的语句，必须另写向前兼容的新迁移（AGENTS.md §4.5）。
-- M0 偏差：主键用 gen_random_uuid()（v4），PG17 无内置 uuidv7()；待引入扩展后统一 UUIDv7。

create extension if not exists pgcrypto;

-- 用户与会话（ADR-34：access 用 JWT，refresh 存哈希，撤销靠 token_version）
create table if not exists users (
    id            uuid primary key default gen_random_uuid(),
    username      text not null unique,
    password_hash text not null,
    role          text not null default 'viewer'
                  check (role in ('admin','operator','viewer')),
    token_version int  not null default 0,
    disabled      boolean not null default false,
    created_at    timestamptz not null default now()
);

create table if not exists refresh_tokens (
    jti        uuid primary key default gen_random_uuid(),
    user_id    uuid not null references users(id) on delete cascade,
    token_hash text not null unique,
    expires_at timestamptz not null,
    revoked_at timestamptz,
    created_at timestamptz not null default now()
);
create index if not exists refresh_tokens_user_idx on refresh_tokens(user_id);

-- 拓扑：region → az → cluster → host
create table if not exists regions (
    id         uuid primary key default gen_random_uuid(),
    name       text not null unique,
    created_at timestamptz not null default now()
);

create table if not exists azs (
    id         uuid primary key default gen_random_uuid(),
    region_id  uuid not null references regions(id) on delete cascade,
    name       text not null,
    created_at timestamptz not null default now(),
    unique (region_id, name)
);

create table if not exists clusters (
    id            uuid primary key default gen_random_uuid(),
    az_id         uuid references azs(id) on delete set null,
    name          text not null,
    fsid          text unique,
    capability    text not null default 'managed' check (capability in ('managed','limited')),
    ceph_version  text,
    image_digest  text,
    health        text,
    drift_state   text not null default 'ok' check (drift_state in ('ok','drift')),
    last_sync_at  timestamptz,
    created_at    timestamptz not null default now()
);

create table if not exists hosts (
    id           uuid primary key default gen_random_uuid(),
    cluster_id   uuid references clusters(id) on delete set null,  -- 可空 = 未分配池
    hostname     text not null,
    mgmt_ip      text not null,
    labels       jsonb not null default '{}'::jsonb,
    agent_status text not null default 'unknown',
    os_version   text,
    created_at   timestamptz not null default now(),
    unique (hostname)
);

-- 任务（ADR-36：任务表即队列，SKIP LOCKED + 租约）
create table if not exists tasks (
    id              uuid primary key default gen_random_uuid(),
    cluster_id      uuid references clusters(id) on delete cascade,
    type            text not null,
    params          jsonb not null default '{}'::jsonb,
    status          text not null default 'PENDING'
                    check (status in ('PENDING','RUNNING','SUCCEEDED','FAILED','RETRYING','CANCELED','DEAD_LETTER')),
    attempt_count   int not null default 0,
    leased_until    timestamptz,
    lease_owner     text,
    next_retry_at   timestamptz not null default now(),
    idempotency_key text unique,
    created_by      uuid references users(id) on delete set null,
    error_code      text,
    error_message   text,
    created_at      timestamptz not null default now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    last_progress_at timestamptz
);

-- 派单只扫可执行的任务，部分索引让扫描与表大小无关
create index if not exists tasks_claimable_idx
    on tasks (next_retry_at, created_at)
    where status in ('PENDING','RETRYING');
create index if not exists tasks_cluster_idx on tasks (cluster_id, created_at desc);
-- 租约回收只扫「RUNNING 且已过期」的行（ADR-36），部分索引让开销与表大小无关
create index if not exists tasks_lease_idx on tasks (leased_until) where status = 'RUNNING';
