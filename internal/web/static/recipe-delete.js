// Enables the final Delete button of a recipe's confirmation page once DELETE
// is typed. The server checks the word as well; this only saves a round trip.
(function () {
  var form = document.querySelector('.delete-confirm form');
  if (!form) return;
  var input = form.elements.confirm;
  var button = form.querySelector('button[type=submit]');
  input.addEventListener('input', function () {
    button.disabled = input.value.trim() !== 'DELETE';
  });
})();
