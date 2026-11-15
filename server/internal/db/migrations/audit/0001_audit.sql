-- 审计库初始结构（ADR-29：两段式 + 链式哈希）
-- 独立库：与业务库分开连接，避免「业务回滚把审计带走」
--
-- 回滚说明：仅适用于全新环境重建：
--   drop table if exists audit_results, audit_intents cascade;
--   drop extension if exists pgcrypto;
-- 审计数据在投产环境不可删除；保留期与归档策略另见 ADR-32。

create extension if not exists pgcrypto;

-- ① intent：写操作前落，失败即拒绝执行（fail-closed）
create table if not exists audit_intents (
    id         bigserial primary key,
    trace_id   uuid not null,
    actor_id   uuid,
    actor_name text,
    action     text not null,
    target     text,
    params     jsonb,
    created_at timestamptz not null default now()
);
create index if not exists audit_intents_trace_idx on audit_intents(trace_id);

-- ② result：操作完成后异步补写，带链式哈希防篡改
create table if not exists audit_results (
    trace_id   uuid primary key,
    status     text not null,
    detail     jsonb,
    prev_hash  text,
    hash       text not null,
    created_at timestamptz not null default now()
);
create index if not exists audit_results_created_idx on audit_results(created_at desc);
