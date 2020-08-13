local Seq = {}
Seq.__index = Seq

function Seq.new(base)
    return setmetatable({_n = base or 0}, Seq)
end

function Seq:head()
    return self._n
end

function Seq:next()
    self._n = self._n + 1
    return self._n
end

function Seq:rand()
    return math.random(self._n)
end

local Range = {}
Range.__index = Range

function Range.new(min, max)
    max = max or 0
    if min > max then
        min, max = min, max
    end
    return setmetatable({min = min, max = max}, Range)
end

function Range:randi()
    return self.min + math.random(0, self.max - self.min)
end

function Range:randf()
    return self.min + (self.max - self.min) * math.random()
end

function Range:randt()
    return timef(self:randi())
end

function choice(items)
    return items[math.random(#items)]
end

return {
    seq = Seq.new,
    range = Range.new,
    choice = choice,
}
