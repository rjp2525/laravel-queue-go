package queue

import "github.com/redis/go-redis/v9"

// Lua scripts that exactly match Laravel's Redis queue implementation.
// These maintain atomicity with Laravel workers running alongside Go workers.

// KEYS[1] = queues:{name}, KEYS[2] = queues:{name}:notify
// ARGV[1] = payload JSON
var luaPush = redis.NewScript(`
redis.call('rpush', KEYS[1], ARGV[1])
redis.call('rpush', KEYS[2], 1)
return true
`)

// KEYS[1] = queues:{name}:delayed, KEYS[2] = queues:{name}:notify
// ARGV[1] = available_at timestamp, ARGV[2] = payload JSON
var luaLater = redis.NewScript(`
redis.call('zadd', KEYS[1], ARGV[1], ARGV[2])
redis.call('rpush', KEYS[2], 1)
return true
`)

// KEYS[1] = queues:{name}, KEYS[2] = queues:{name}:reserved
// ARGV[1] = reserved_until timestamp
var luaPop = redis.NewScript(`
local job = redis.call('lpop', KEYS[1])
if job ~= false then
    redis.call('zadd', KEYS[2], ARGV[1], job)
    return job
end
return false
`)

// KEYS[1] = queues:{name}:delayed, KEYS[2] = queues:{name}:reserved
// ARGV[1] = payload, ARGV[2] = available_at timestamp
var luaRelease = redis.NewScript(`
redis.call('zrem', KEYS[2], ARGV[1])
redis.call('zadd', KEYS[1], ARGV[2], ARGV[1])
return true
`)

// luaDelete removes a job from the reserved set (ACK).
// KEYS[1] = queues:{name}:reserved
// ARGV[1] = payload
var luaDelete = redis.NewScript(`
redis.call('zrem', KEYS[1], ARGV[1])
return true
`)

// KEYS[1] = from (delayed or reserved), KEYS[2] = to (ready), KEYS[3] = notify
// ARGV[1] = currentTime, ARGV[2] = batch size
var luaMigrate = redis.NewScript(`
local val = redis.call('zrangebyscore', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
if next(val) ~= nil then
    redis.call('zremrangebyrank', KEYS[1], 0, #val - 1)
    for i = 1, #val do
        redis.call('rpush', KEYS[2], val[i])
        redis.call('rpush', KEYS[3], 1)
    end
end
return val
`)

// KEYS[1] = queues:{name}, KEYS[2] = delayed, KEYS[3] = reserved, KEYS[4] = notify
var luaClear = redis.NewScript(`
local size = redis.call('llen', KEYS[1]) + redis.call('zcard', KEYS[2]) + redis.call('zcard', KEYS[3])
redis.call('del', KEYS[1], KEYS[2], KEYS[3], KEYS[4])
return size
`)
