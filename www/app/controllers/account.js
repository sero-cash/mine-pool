import Ember from 'ember';

export default Ember.Controller.extend({
  applicationController: Ember.inject.controller('application'),
  stats: Ember.computed.reads('applicationController.model.stats'),

  roundPercent: Ember.computed('stats', 'model', {
    get() {
      var percent = this.get('model.roundShares') / this.get('stats.roundShares');
      if (!percent) {
        return 0;
      }
      return percent;
    }
  }),

  blocksFound: Ember.computed('model', {
    get() {
      if (typeof this.get('model.stats.blocksFound') === 'undefined') {
        return 0;
      } else {
        return this.get('model.stats.blocksFound');
      }
    }
  })
});
