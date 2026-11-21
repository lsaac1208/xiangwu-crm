const pad = (value) => String(value).padStart(2, '0')

export const datePartsFromDate = (date) => [
  String(date.getFullYear()),
  pad(date.getMonth() + 1),
  pad(date.getDate())
]

export const timePartsFromDate = (date) => [
  pad(date.getHours()),
  pad(date.getMinutes())
]

export const combineFollowupDateParts = (dateParts, timeParts) => {
  const [year, month, day] = dateParts.map(Number)
  const [hour, minute] = timeParts.map(Number)
  return new Date(year, month - 1, day, hour, minute, 0, 0)
}

export const formatFollowupDateTime = (date) => {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}
