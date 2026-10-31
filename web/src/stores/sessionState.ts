import { reactive, ref } from 'vue';
// Shared session identity and mutation gate; no form secrets belong here.
export const auth = reactive({
  ready: false,
  loggedIn: false,
  version: '2.2.0',
  error: '',
  epoch: 0,
});
export const busy = ref(false);
