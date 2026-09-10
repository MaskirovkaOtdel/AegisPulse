package ratelimit

const SlidingWindowLuaScript = `
local ratelimit_key = KEYS[1]
local billing_key   = KEYS[2]
local stream_key    = KEYS[3]

local now           = tonumber(ARGV[1])
local window        = tonumber(ARGV[2])
local soft_limit    = tonumber(ARGV[3])
local hard_limit    = tonumber(ARGV[4])
local req_id        = ARGV[5]
local api_key       = ARGV[6]
local tier          = ARGV[7]
local burst_frozen  = tonumber(ARGV[8])

-- 1. Prune entries older than (now - window)
local expire_threshold = now - window
redis.call('ZREMRANGEBYSCORE', ratelimit_key, 0, expire_threshold)

-- 2. Count requests currently in sliding window
local count = redis.call('ZCARD', ratelimit_key)

-- 3. Evaluate dual thresholds
if count < soft_limit then
    redis.call('ZADD', ratelimit_key, now, req_id)
    redis.call('EXPIRE', ratelimit_key, window)
    local remaining = soft_limit - (count + 1)
    local burst_rem = hard_limit - soft_limit
    return {'NORMAL', tostring(count + 1), tostring(soft_limit), tostring(hard_limit), tostring(remaining), tostring(burst_rem)}
elseif count < hard_limit and burst_frozen == 0 then
    redis.call('ZADD', ratelimit_key, now, req_id)
    redis.call('EXPIRE', ratelimit_key, window)
    local overage_units = redis.call('HINCRBY', billing_key, 'units', 1)
    redis.call('XADD', stream_key, '*', 'event_type', 'BURST_OVERAGE', 'api_key', api_key, 'tier', tier, 'timestamp', tostring(now), 'units', tostring(overage_units))
    local burst_rem = hard_limit - (count + 1)
    return {'BURST', tostring(count + 1), tostring(soft_limit), tostring(hard_limit), '0', tostring(burst_rem)}
else
    local status = 'BLOCKED'
    return {status, tostring(count), tostring(soft_limit), tostring(hard_limit), '0', '0'}
end
`
