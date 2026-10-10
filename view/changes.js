// Order of the sidebar's change lists: deleted changes first, then the rest
// in the order the list already had. The sort is stable, so each group keeps
// its own order rules (by name, today).

export function deletedFirst(list, changeOf = (item) => item.change) {
  return list
    .map((item, index) => ({ item, index, gone: changeOf(item) === "removed" ? 0 : 1 }))
    .sort((a, b) => a.gone - b.gone || a.index - b.index)
    .map((entry) => entry.item);
}
