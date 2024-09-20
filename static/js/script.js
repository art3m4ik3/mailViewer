document.addEventListener('DOMContentLoaded', (event) => {
    const sendEmailForm = document.querySelector('form[action="/send_email"]');
    if (sendEmailForm) {
        sendEmailForm.addEventListener('submit', (e) => {
            if (!confirm('Вы уверены, что хотите отправить это письмо?')) {
                e.preventDefault();
            }
        });
    }
});