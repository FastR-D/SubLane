-- SQLite cannot alter CHECK constraints; rebuild request history to accept image operations.
CREATE TABLE request_records_next (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id INTEGER NOT NULL,
 key_id INTEGER NOT NULL,
 group_id INTEGER NOT NULL,
 account_id TEXT NOT NULL DEFAULT '',
 provider TEXT NOT NULL DEFAULT '',
 model TEXT NOT NULL DEFAULT '' CHECK(length(model)<=160),
 transport TEXT NOT NULL CHECK(transport IN ('http','websocket')),
 operation TEXT NOT NULL CHECK(operation IN ('responses','chat','compact','messages','gemini','images')),
 started_at INTEGER NOT NULL,
 duration_ms INTEGER NOT NULL CHECK(duration_ms>=0),
 outcome TEXT NOT NULL CHECK(outcome IN ('success','incomplete','error','canceled','rejected')),
 error_code TEXT NOT NULL DEFAULT '' CHECK(length(error_code)<=64),
 upstream_status INTEGER,
 input_tokens INTEGER,
 output_tokens INTEGER,
 cached_tokens INTEGER,
 request_id TEXT NOT NULL DEFAULT '' CHECK(length(request_id)<=64),
 first_token_ms INTEGER CHECK(first_token_ms>=0),
 reasoning_effort TEXT NOT NULL DEFAULT ''
 CHECK(reasoning_effort IN ('','none','minimal','low','medium','high','xhigh','max','ultra')),
 content_mode TEXT NOT NULL DEFAULT '' CHECK(content_mode IN ('','observe','block')),
 content_revision INTEGER NOT NULL DEFAULT 0,
 content_rule_ids TEXT NOT NULL DEFAULT '[]' CHECK(length(content_rule_ids)<=4096 AND json_valid(content_rule_ids)),
 content_check_failed INTEGER NOT NULL DEFAULT 0 CHECK(content_check_failed IN (0,1)),
 route_explanation TEXT NOT NULL DEFAULT 'null'
 CHECK(length(route_explanation)<=1024 AND json_valid(route_explanation))
);

INSERT INTO request_records_next(id,user_id,key_id,group_id,account_id,provider,model,transport,operation,started_at,duration_ms,outcome,error_code,upstream_status,input_tokens,output_tokens,cached_tokens,request_id,first_token_ms,reasoning_effort,content_mode,content_revision,content_rule_ids,content_check_failed,route_explanation)
SELECT id,user_id,key_id,group_id,account_id,provider,model,transport,operation,started_at,duration_ms,outcome,error_code,upstream_status,input_tokens,output_tokens,cached_tokens,request_id,first_token_ms,reasoning_effort,content_mode,content_revision,content_rule_ids,content_check_failed,route_explanation
FROM request_records;

DROP TABLE request_records;
ALTER TABLE request_records_next RENAME TO request_records;

CREATE INDEX request_records_account ON request_records(account_id,id);
CREATE INDEX request_records_request_id ON request_records(request_id) WHERE request_id!='';
CREATE INDEX request_records_time ON request_records(started_at);
