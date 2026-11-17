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

// KEYS[1] = queues:{name}:delayed
// ARGV[1] = available_at timestamp, ARGV[2] = payload JSON
//
// Delayed jobs are not announced on the notify list here: the migrate script
// pushes one notification per job when it moves them to the ready list.
var luaLater = redis.NewScript(`
redis.call('zadd', KEYS[1], ARGV[1], ARGV[2])
return true
`)

// luaPop is Laravel's LuaScripts::pop(), unchanged across Laravel 10-13.
// It increments "attempts" inside Redis and stores that re-encoded payload in
// the reserved set, so a job whose worker dies still counts the attempt. It
// returns {job, reserved}: the payload as pushed and the reserved member.
//
// KEYS[1] = queues:{name}, KEYS[2] = queues:{name}:reserved, KEYS[3] = queues:{name}:notify
// ARGV[1] = reserved_until timestamp
var luaPop = redis.NewScript(`
local job = redis.call('lpop', KEYS[1])
local reserved = false

if(job ~= false) then
    reserved = cjson.decode(job)
    reserved['attempts'] = reserved['attempts'] + 1
    reserved = cjson.encode(reserved)
    redis.call('zadd', KEYS[2], ARGV[1], reserved)
    redis.call('lpop', KEYS[3])
end

return {job, reserved}
`)

// KEYS[1] = queues:{name}:delayed, KEYS[2] = queues:{name}:reserved
// ARGV[1] = reserved payload, ARGV[2] = available_at timestamp
var luaRelease = redis.NewScript(`
redis.call('zrem', KEYS[2], ARGV[1])
redis.call('zadd', KEYS[1], ARGV[2], ARGV[1])
return true
`)

// luaDelete removes a job from the reserved set (ACK).
// KEYS[1] = queues:{name}:reserved
// ARGV[1] = reserved payload
var luaDelete = redis.NewScript(`
redis.call('zrem', KEYS[1], ARGV[1])
return true
`)

// luaMigrate moves due jobs from a sorted set to the ready list, pushing them
// in chunks of 100 as Laravel's LuaScripts::migrateExpiredJobs() does. Unlike
// Laravel's script it returns the number of jobs moved rather than their
// payloads, which nothing here reads and which would otherwise be sent back
// over the network on every migration.
//
// KEYS[1] = from (delayed or reserved), KEYS[2] = to (ready), KEYS[3] = notify
// ARGV[1] = currentTime, ARGV[2] = batch size (negative = no limit)
var luaMigrate = redis.NewScript(`
local val = redis.call('zrangebyscore', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
if next(val) ~= nil then
    redis.call('zremrangebyrank', KEYS[1], 0, #val - 1)
    for i = 1, #val, 100 do
        local last = math.min(i + 99, #val)
        redis.call('rpush', KEYS[2], unpack(val, i, last))
        for j = i, last do
            redis.call('rpush', KEYS[3], 1)
        end
    end
end
return #val
`)

// KEYS[1] = queues:{name}, KEYS[2] = delayed, KEYS[3] = reserved, KEYS[4] = notify
var luaClear = redis.NewScript(`
local size = redis.call('llen', KEYS[1]) + redis.call('zcard', KEYS[2]) + redis.call('zcard', KEYS[3])
redis.call('del', KEYS[1], KEYS[2], KEYS[3], KEYS[4])
return size
`)

// luaSize is Laravel's LuaScripts::size().
// KEYS[1] = queues:{name}, KEYS[2] = delayed, KEYS[3] = reserved
var luaSize = redis.NewScript(`
return redis.call('llen', KEYS[1]) + redis.call('zcard', KEYS[2]) + redis.call('zcard', KEYS[3])
`)
