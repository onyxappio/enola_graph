import { loadUser } from "./io";

export function showUser(id: string) {
  return loadUser(id);
}
