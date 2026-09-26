export function addPair(pairs, pair) {
  if (pairs.length >= 4 || pairs.some((item) => item.store === pair.store && item.model === pair.model)) {
    return pairs;
  }
  return [...pairs, pair];
}
