/* Terraform Configuration tab: registers the renderer into the drawer tab registry. */
(function () {
  'use strict';
  const TFUI = window.TfUI = window.TfUI || {};
  tfTabRenderers.config = function (body) {
    return (tfWs && tfWs.source === 'git') ? TFUI.cfg.renderGit(body) : TFUI.cfg.render(body);
  };
})();
