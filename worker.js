export default {
  async email(message, env, ctx) {
    const toAddress = message.to.toLowerCase();
    console.log(toAddress)
    if (toAddress.match(/^users\+\d{1,8}tgopi.com$/)) {
      const subject = message.headers.get('subject');
      const codeMatch = subject.match(/Slack confirmation code: ([A-Z0-9-]+)/);
      
      if (codeMatch) {
        const verificationCode = {
          email: toAddress,
          code: codeMatch[1]
        };

        try {
          const response = await fetch("SERVER_URL/webhook", {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
            },
            body: JSON.stringify(verificationCode),
          });

          if (response.ok) {
            console.log(`Successfully sent verification code for ${toAddress}`);
          } else {
            console.error(`Failed to send verification code for ${toAddress}. Status: ${response.status}`);
          }
        } catch (error) {
          console.error(`Error sending verification code for ${toAddress}: ${error.message}`);
        }
      } else {
        console.error(`Failed to extract verification code from subject: ${subject}`);
      }
    } else {
      message.setReject("Unknown address");
    }
  }
}