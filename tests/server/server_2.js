const { Server } = require("socket.io");

const io = new Server(2233, {});

io.on("connection", (socket) => {
  console.log("client connected");

  setTimeout(() => {
    socket.disconnect();
  }, 500);

  socket.on("message", function (...args) {
    console.log("client sent message:", ...args);
  });

  socket.on("disconnect", (reason) => {
    console.log(`client disconnect because ${reason}`); // client disconnect because transport close
  });
});

console.log("listening...");
