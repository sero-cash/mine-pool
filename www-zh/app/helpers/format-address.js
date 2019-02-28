import Ember from 'ember';

export function formatAdress(value) {
  return value[0].substring(0,16) + "..." + value[0].substring(42);
}

export default Ember.Helper.helper(formatAdress);
