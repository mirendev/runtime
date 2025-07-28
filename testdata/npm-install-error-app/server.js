const express = require('express');
const app = express();
const port = process.env.PORT || 3000;

app.get('/', (req, res) => {
  res.send('Hello from npm install error test app!');
});

app.get('/health', (req, res) => {
  res.json({ status: 'healthy', message: 'This app should never run due to npm install failures' });
});

app.listen(port, () => {
  console.log(`Server running on port ${port}`);
  console.log('Note: This should never actually run due to failed npm install');
});