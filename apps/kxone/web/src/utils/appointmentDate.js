// Appointment inputs and calendar boundaries always use Asia/Shanghai.
export const businessDate = value => new Date(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2})(:\d{2})?$/.test(value) ? `${value}+08:00` : value)
export const localDateTime = value => new Date(businessDate(value).getTime() + 8 * 3600000).toISOString().slice(0, 16)
export const localDateKey = value => localDateTime(value).slice(0, 10)
export const businessISO = value => businessDate(value).toISOString()
