import type { ObjectDirective } from 'vue';

const values = new WeakMap<HTMLInputElement, string>();

/** Password managers may ignore autocomplete=off. Keep search read-only until
 * a real pointer/key interaction; reject unsolicited input without changing v-model. */
export const vSearchGuard: ObjectDirective<HTMLInputElement, string> = {
  beforeMount(input) {
    values.set(input, input.value);
    input.readOnly = true;
    const unlock = (event: Event) => {
      if (event.isTrusted) input.readOnly = false;
    };
    input.addEventListener('pointerdown', unlock);
    input.addEventListener('keydown', unlock);
    input.addEventListener('blur', () => {
      input.readOnly = true;
    });
    input.addEventListener(
      'input',
      (event) => {
        if (input.readOnly) {
          input.value = values.get(input) || '';
          event.stopImmediatePropagation();
        } else {
          values.set(input, input.value);
        }
      },
      true
    );
  },
  updated(input) {
    values.set(input, input.value);
  },
};
