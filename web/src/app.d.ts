// see https://svelte.dev/docs/kit/types#app.d.ts
declare global {
  namespace App {
    interface Error {
      status: number;
      message: string;
      // stable code for ui copy. optional.
      code?: string;
    }
    // interface Locals {}
    // interface PageData {}
    // interface PageState {}
    // interface Platform {}
  }
}

export {};
