const { Server } = require("socket.io");

const io = new Server(2233, {});

io.on("connection", (socket) => {
  console.log("client connected");

  setTimeout(() => {
    socket.emit("message", "hello world");
    setTimeout(() => {
      socket.emit("message", "goodbye world");
    }, 50);
  }, 50);

  socket.on("message", function (...args) {
    console.log("client sent message:", ...args);
  });

  socket.on("disconnect", (reason) => {
    console.log(`client disconnect because ${reason}`); // client disconnect because transport close
  });
});

console.log("listening...");
