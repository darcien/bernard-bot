declare module "https://esm.sh/e25n@1.1.0" {
  /**
   * Determine what the English Numerical Contraction is for an English word or phrase and view known collisions.
   * Demo: https://encapsulate.me/writing/e25n.html
   * @deprecated DO NOT USE YET, IMPORT BROKEN
   */
  export default function e25n(input: string): {
    newWord: string;
    collisions: Array<string>;
    annoyingScale: 1 | 2 | 3 | 4 | 5;
  };
}
