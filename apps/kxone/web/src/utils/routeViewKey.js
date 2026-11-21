// Reuse a page for query changes, but replace it when the customer changes.
export const routeViewKey = route => {
  const id = route?.params?.id
  return id !== undefined && id !== null && id !== '' ? `${route.name || 'route'}:${id}` : route?.path || ''
}
