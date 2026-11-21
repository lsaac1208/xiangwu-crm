package cache

import "context"

const loginProtectionScript = `
local tm=redis.call('TIME'); local now=tonumber(tm[1])
if ARGV[1]=='success' then redis.call('DEL',KEYS[1]); return 0 end
local untilAt=tonumber(redis.call('HGET',KEYS[1],'until') or '0')
if untilAt>now then return untilAt-now end
if ARGV[1]=='check' then return 0 end
local first=tonumber(redis.call('HGET',KEYS[1],'first') or '0')
local count=tonumber(redis.call('HGET',KEYS[1],'count') or '0')
local level=tonumber(redis.call('HGET',KEYS[1],'level') or '0')
if first==0 or now-first>=600 then first=now; count=0; level=0 end
count=count+1
local delay=0
if count>=10 then level=math.min(level+1,4); local delays={30,60,120,300}; delay=delays[level] end
redis.call('HSET',KEYS[1],'first',first,'count',count,'level',level,'until',now+delay)
redis.call('EXPIRE',KEYS[1],900)
return delay
`

func (c *RedisCache) LoginAttempt(ctx context.Context, key, operation string) (int64, error) {
	return c.client.Eval(ctx, loginProtectionScript, []string{key}, operation).Int64()
}
