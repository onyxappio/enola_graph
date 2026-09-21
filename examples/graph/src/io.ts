export function loadUser(id: string) {
  return fetch("/users/" + id);
}
