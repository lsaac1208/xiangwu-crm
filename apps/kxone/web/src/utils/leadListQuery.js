export const queryValue = value => Array.isArray(value) ? value[0] : typeof value === 'string' ? value : ''
export const queryFingerprint = query => JSON.stringify(Object.keys(query).sort().map(key => [key, queryValue(query[key])]))
